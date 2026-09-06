// Command faultproxy is a stage-06 integration-trial tool (docs/開發流程框架.html
// 階段06「整合試跑」): a reverse proxy sitting between UCollection and a real
// TronGrid endpoint, letting a tester flip in failure modes (disconnect,
// timeout, 5xx, 429, slow) on demand to observe how internal/scanner,
// internal/tronclient, and internal/admin's reverify path behave under
// each condition. Point TRONGRID_BASE_URL at this proxy's address instead
// of the real TronGrid host.
//
// Not part of the production build — run directly with `go run
// scripts/faultproxy/main.go`, never imported by cmd/ucollection.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type mode string

const (
	modePassthrough mode = "passthrough"
	modeDisconnect  mode = "disconnect"
	modeTimeout     mode = "timeout"
	modeHTTP5xx     mode = "http5xx"
	modeHTTP429     mode = "http429"
	modeSlow        mode = "slow"
)

type controller struct {
	mu        sync.RWMutex
	current   mode
	slowDelay time.Duration
}

func (c *controller) get() (mode, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current, c.slowDelay
}

func (c *controller) set(m mode, slowDelay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = m
	if slowDelay > 0 {
		c.slowDelay = slowDelay
	}
}

func main() {
	listenAddr := flag.String("listen", ":8093", "address for the proxy to listen on")
	upstream := flag.String("upstream", "https://api.shasta.trongrid.io", "real TronGrid base URL to forward passthrough requests to")
	adminAddr := flag.String("admin-listen", ":8094", "address for the /fault-mode control endpoint")
	flag.Parse()

	upstreamURL, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("invalid -upstream: %v", err)
	}

	ctrl := &controller{current: modePassthrough, slowDelay: 5 * time.Second}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstreamURL)
			r.Out.Host = upstreamURL.Host
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		m, slowDelay := ctrl.get()
		switch m {
		case modeDisconnect:
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "cannot hijack", http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			_ = conn.Close()
			log.Printf("[disconnect] %q %q -> connection closed", r.Method, r.URL.Path) //nolint:gosec // local test-only tool logging its own inbound request line for console debugging, not a real log-injection boundary
		case modeTimeout:
			log.Printf("[timeout] %q %q -> hanging until client gives up", r.Method, r.URL.Path) //nolint:gosec // same as above
			<-r.Context().Done()
		case modeHTTP5xx:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"stage06 faultproxy: simulated 503"}`))
			log.Printf("[http5xx] %q %q -> 503", r.Method, r.URL.Path) //nolint:gosec // same as above
		case modeHTTP429:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"stage06 faultproxy: simulated 429"}`))
			log.Printf("[http429] %q %q -> 429", r.Method, r.URL.Path) //nolint:gosec // local test-only tool logging its own inbound request line for console debugging, not a real log-injection boundary
		case modeSlow:
			log.Printf("[slow] %q %q -> delaying %s then forwarding", r.Method, r.URL.Path, slowDelay) //nolint:gosec // same as above
			select {
			case <-time.After(slowDelay):
				proxy.ServeHTTP(w, r)
			case <-r.Context().Done():
			}
		default: // passthrough
			proxy.ServeHTTP(w, r)
		}
	})

	server := &http.Server{Addr: *listenAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/fault-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			m, d := ctrl.get()
			_, _ = w.Write([]byte(`{"mode":"` + string(m) + `","slow_delay_seconds":` + strconv.Itoa(int(d.Seconds())) + `}`))
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m := mode(r.FormValue("mode"))
		switch m {
		case modePassthrough, modeDisconnect, modeTimeout, modeHTTP5xx, modeHTTP429, modeSlow:
		default:
			http.Error(w, "unknown mode: "+string(m), http.StatusBadRequest)
			return
		}
		var slowDelay time.Duration
		if raw := r.FormValue("slow_delay_seconds"); raw != "" {
			secs, err := strconv.Atoi(raw)
			if err != nil || secs <= 0 {
				http.Error(w, "invalid slow_delay_seconds", http.StatusBadRequest)
				return
			}
			slowDelay = time.Duration(secs) * time.Second
		}
		ctrl.set(m, slowDelay)
		log.Printf("fault mode switched to %q (slow_delay=%s)", m, slowDelay) //nolint:gosec // m is already constrained to the fixed enum validated above
		_, _ = w.Write([]byte(`{"ok":true,"mode":"` + string(m) + `"}`)) //nolint:gosec // m is already constrained to the fixed enum validated in the switch above
	})
	adminServer := &http.Server{Addr: *adminAddr, Handler: adminMux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("faultproxy: proxying %s -> upstream %s", *listenAddr, upstreamURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("proxy server: %v", err)
		}
	}()

	log.Printf("faultproxy: admin control endpoint on %s (POST /fault-mode {mode: passthrough|disconnect|timeout|http5xx|http429|slow})", *adminAddr)
	if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("admin server: %v", err)
	}
}
