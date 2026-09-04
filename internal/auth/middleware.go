package auth

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type contextKey int

const sessionContextKey contextKey = iota

// SessionFromContext retrieves the Session that RequireSession attached to
// the request context. Handlers wrapped by RequireSession can rely on this
// always succeeding.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(Session)
	return s, ok
}

// RequireSession implements 技術架構設計第9節「Session模型」: the
// status='active' AND not-expired AND not-idle three-condition check, plus
// the Origin-header CSRF check bundled into this same middleware (第9節
// 「CSRF防護」). pending_2fa sessions are rejected here by construction —
// lookupActiveSession only accepts status='active' — matching 第9節's
// explicit requirement that pending_2fa sessions can't reach any
// /admin/... route besides the login flow itself (which doesn't use this
// middleware).
func RequireSession(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" && origin != deps.PublicOrigin {
				http.Error(w, "origin mismatch", http.StatusForbidden)
				return
			}

			token, ok := sessionTokenFromRequest(r)
			if !ok {
				redirectToLogin(w, r)
				return
			}

			sess, ok, err := lookupActiveSession(r.Context(), deps.Pool, token)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				clearSessionCookie(w, deps.CookieSecure)
				redirectToLogin(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireFreshTOTP implements 技術架構設計第9節「高風險操作Step-up驗證」. Must be
// composed after RequireSession (it reads the session from context, not
// the cookie). A stale last_totp_verified_at redirects to
// /admin/reverify-totp with the original request's URI preserved so the
// handler can send the operator back after they re-enter a TOTP code.
func RequireFreshTOTP(_ Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := SessionFromContext(r.Context())
			if !ok {
				http.Error(w, "internal error: RequireFreshTOTP used without RequireSession", http.StatusInternalServerError)
				return
			}

			fresh := sess.LastTOTPVerifiedAt != nil && time.Since(*sess.LastTOTPVerifiedAt) < freshTOTPWindow
			if !fresh {
				target := "/admin/reverify-totp?next=" + url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
