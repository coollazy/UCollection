package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// ipRateLimitWindow/ipRateLimitMax implement 技術架構設計第9節「登入限流」第1層:
// 10 failures across the four auth endpoints per IP within 15 minutes.
const (
	ipRateLimitWindow = 15 * time.Minute
	ipRateLimitMax    = 10
)

// ipLimiter is process-memory-only, deliberately not persisted to the DB
// (技術架構設計第9節: "process記憶體內，不落地DB，符合單一process常駐假設，重啟後
// 計數器歸零可接受"). Pruning happens lazily per accessed key on each call;
// entries for IPs that stop attempting are never proactively swept — an
// accepted limitation for this system's threat model (see plan judgment
// point 6), not a fix warranting a background sweep goroutine.
type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{hits: make(map[string][]time.Time)}
}

// sharedIPLimiter is the one per-IP failure budget every TOTP-verifying
// endpoint draws from — login, totp-setup, totp, /admin/reverify-totp
// (all wired up in RegisterRoutes), and RequireTOTPCode's inline
// verification (used by internal/admin and internal/consolidation, which
// have no other way to reach the limiter RegisterRoutes constructs). 技術
// 架構設計第9節「登入限流」第1層 is explicitly one shared 15-minute budget
// across every rate-limited endpoint, not one budget per endpoint.
var sharedIPLimiter = newIPLimiter()

// allow reports whether ip is currently under its 15-minute failure quota.
func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(ip, time.Now())) < ipRateLimitMax
}

// recordFailure records one failed attempt for ip.
func (l *ipLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	hits := l.prune(ip, now)
	l.hits[ip] = append(hits, now)
}

// prune must be called with l.mu held; it drops entries older than the
// window and writes the pruned slice back before returning it.
func (l *ipLimiter) prune(ip string, now time.Time) []time.Time {
	cutoff := now.Add(-ipRateLimitWindow)
	hits := l.hits[ip]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, ip)
		return nil
	}
	l.hits[ip] = kept
	return kept
}

// clientIP uses the raw TCP source (r.RemoteAddr), not X-Forwarded-For or
// similar headers — this project has no built-in reverse proxy (ADR-0004)
// and no configured trusted-proxy allowlist, so any such header is
// attacker-controllable and would let the IP layer be bypassed outright.
// Known limitation: if the operator does deploy behind a reverse proxy,
// all real clients share one apparent IP and this layer's quota becomes a
// shared budget — the per-session layer (5 consecutive TOTP failures,
// see session.go) still holds independently of this.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
