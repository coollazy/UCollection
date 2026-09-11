// Command ucollection is the UCollection entry point: it loads config, opens
// the database, runs migrations, starts the on-chain scanner background
// task, and serves HTTP. See docs/開發流程框架-03-技術架構設計.md 第1節.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/coollazy/UCollection/internal/admin"
	"github.com/coollazy/UCollection/internal/api"
	"github.com/coollazy/UCollection/internal/auth"
	"github.com/coollazy/UCollection/internal/checkout"
	"github.com/coollazy/UCollection/internal/config"
	"github.com/coollazy/UCollection/internal/consolidation"
	"github.com/coollazy/UCollection/internal/scanner"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
	"github.com/coollazy/UCollection/internal/webhook"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	migrateCtx, cancelMigrate := context.WithTimeout(ctx, 30*time.Second)
	defer cancelMigrate()
	if err := store.Migrate(migrateCtx, cfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Must happen before the HTTP server starts accepting traffic that
	// could create orders (技術架構設計第4節「首次部署SOP」) — not left to
	// scanner.Run's own goroutine, which only starts after ListenAndServe.
	if err := scanner.EnsureCheckpoint(ctx, pool); err != nil {
		return err
	}
	if err := auth.EnsureAdminAccount(ctx, pool, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return err
	}
	tronClient := tronclient.NewClient(cfg.TronGridBaseURL, cfg.TronGridAPIKey)

	authDeps := auth.Deps{Pool: pool, PublicOrigin: cfg.PublicOrigin, CookieSecure: cfg.CookieSecure}
	consolidationDeps := consolidation.Deps{Pool: pool, TronClient: tronClient, USDTContractAddress: cfg.USDTContractAddress}
	adminDeps := admin.Deps{Pool: pool, TronClient: tronClient, USDTContractAddress: cfg.USDTContractAddress}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(pool))
	mux.Handle("/api/v1/", api.NewMux(pool, cfg.PublicOrigin))
	// web/static/ is copied to /web/static in the container image (see
	// Dockerfile) and the binary runs with / as its working directory, so
	// this relative path resolves correctly both locally (go run from the
	// repo root) and in production.
	mux.Handle("GET /static/", staticCacheControl(http.StripPrefix("/static/", http.FileServer(http.Dir("web/static")))))
	auth.RegisterRoutes(mux, authDeps)
	consolidation.RegisterRoutes(mux, consolidationDeps, authDeps)
	admin.RegisterRoutes(mux, adminDeps, authDeps)
	// Public payment page (技術架構設計第6節). No auth — the unguessable
	// public_token is the only access control.
	checkout.RegisterRoutes(mux, checkout.Deps{Pool: pool})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(4)

	var scannerErr error
	go func() {
		defer wg.Done()
		deps := scanner.Deps{Pool: pool, TronClient: tronClient, ContractAddress: cfg.USDTContractAddress}
		if err := scanner.Run(ctx, deps); err != nil && !errors.Is(err, context.Canceled) {
			scannerErr = err
			stop()
		}
	}()

	var webhookErr error
	go func() {
		defer wg.Done()
		if err := webhook.Run(ctx, webhook.Deps{Pool: pool}); err != nil && !errors.Is(err, context.Canceled) {
			webhookErr = err
			stop()
		}
	}()

	var authErr error
	go func() {
		defer wg.Done()
		if err := auth.Run(ctx, authDeps); err != nil && !errors.Is(err, context.Canceled) {
			authErr = err
			stop()
		}
	}()

	var serveErr error
	go func() {
		defer wg.Done()
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = err
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}

	wg.Wait()

	if serveErr != nil {
		return serveErr
	}
	if scannerErr != nil {
		return scannerErr
	}
	if webhookErr != nil {
		return webhookErr
	}
	return authErr
}

// staticCacheControl 在 /static/ 回應設 Cache-Control: no-cache，讓瀏覽器每次
// 使用前先向伺服器重新驗證（配合 FileServer 既有的 Last-Modified/
// If-Modified-Since：未變回 304、不重抓 body；變了回 200 新版），取代原本無
// Cache-Control 時的啟發式快取——那會讓商戶更新系統後仍看到舊的 JS（第16項）。
// 同一版本 container 運行期間檔案 mtime 固定、走 304；商戶更新（重建 container）
// 使 mtime 變動，瀏覽器就會抓到新版。只是一個薄 wrapper，僅掛在 /static/。
func staticCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func healthzHandler(pool *store.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
