package auth

import (
	"context"
	"net/http"

	"github.com/coollazy/UCollection/internal/store"
)

// Deps are the dependencies auth's handlers, middleware and background
// task need.
type Deps struct {
	Pool *store.Pool
	// PublicOrigin is compared against the request Origin header by
	// RequireSession (技術架構設計第9節「CSRF防護」).
	PublicOrigin string
	// CookieSecure controls the session cookie's Secure attribute.
	CookieSecure bool
}

// RegisterRoutes registers auth's exact-path routes directly onto the
// shared top-level mux (not mounted as a "/admin/" prefix handler) — the
// admin module will register its own /admin/... routes on the same mux
// later, and Go 1.22 http.ServeMux's exact-pattern registration lets both
// coexist without either owning the whole prefix.
func RegisterRoutes(mux *http.ServeMux, deps Deps) {
	// Shared across every rate-limited endpoint on purpose (技術架構設計第9節
	// 「登入限流」第1層: /admin/login、/admin/login/totp、
	// /admin/login/totp-setup、/admin/reverify-totp、RequireTOTPCode's inline
	// verification all share one 15-minute per-IP failure budget — see
	// sharedIPLimiter's doc comment.
	limiter := sharedIPLimiter

	mux.HandleFunc("GET /admin/login", loginPageHandler(deps))
	mux.HandleFunc("POST /admin/login", loginSubmitHandler(deps, limiter))

	mux.HandleFunc("GET /admin/login/totp-setup", totpSetupPageHandler(deps))
	mux.HandleFunc("POST /admin/login/totp-setup", totpSetupSubmitHandler(deps, limiter))

	mux.HandleFunc("GET /admin/login/totp", totpVerifyPageHandler(deps))
	mux.HandleFunc("POST /admin/login/totp", totpVerifySubmitHandler(deps, limiter))

	mux.Handle("GET /admin/reverify-totp", RequireSession(deps)(reverifyPageHandler(deps)))
	mux.Handle("POST /admin/reverify-totp", RequireSession(deps)(reverifySubmitHandler(deps, limiter)))

	mux.HandleFunc("POST /admin/logout", logoutHandler(deps))

	mux.Handle("GET /admin/password", RequireSession(deps)(RequireTOTPCode(deps, SelfPath)(passwordPageHandler(deps))))
	mux.Handle("POST /admin/password", RequireSession(deps)(RequireTOTPCode(deps, func(*http.Request) string { return "/admin/password" })(passwordSubmitHandler(deps))))
}

// Run starts the session-cleanup ticker (技術架構設計第9節) and blocks until
// ctx is cancelled. Unlike internal/scanner.Run/internal/webhook.Run,
// there's only one background task here, so there's no multi-goroutine
// fan-out to orchestrate.
func Run(ctx context.Context, deps Deps) error {
	runSessionCleanupTask(ctx, deps.Pool)
	return ctx.Err()
}
