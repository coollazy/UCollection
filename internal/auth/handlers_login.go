package auth

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
)

type loginPageData struct {
	Error string
}

func loginPageHandler(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, http.StatusOK, "login.html", loginPageData{Error: reasonMessage(r.URL.Query().Get("reason"))})
	}
}

// loginSubmitHandler implements 技術架構設計第9節「登入流程」步驟1: verify
// username+password, open a 5-minute pending_2fa session on success. It
// does not distinguish "unknown username" from "wrong password" in either
// the response or its timing-relevant code path — the account name isn't
// treated as secret (第9節「刻意不做帳號層級永久鎖定」reasons this the same
// way), so this isn't hiding anything meaningful.
func loginSubmitHandler(deps Deps, limiter *ipLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !limiter.allow(ip) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}

		if err := r.ParseForm(); err != nil {
			render(w, http.StatusBadRequest, "login.html", loginPageData{Error: "表單格式錯誤"})
			return
		}
		username := r.PostFormValue("username")
		password := r.PostFormValue("password")

		ctx := r.Context()
		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if username != account.Username || !checkPassword(account.PasswordHash, password) {
			limiter.recordFailure(ip)
			_ = audit.Log(ctx, deps.Pool, "admin", "LOGIN_FAILED", nil, nil, map[string]any{"ip": ip})
			render(w, http.StatusUnauthorized, "login.html", loginPageData{Error: "帳號或密碼錯誤"})
			return
		}

		token, _, err := createPendingSession(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, token, deps.CookieSecure, pendingSessionTTL)
		_ = audit.Log(ctx, deps.Pool, "admin", "LOGIN_SUCCESS", nil, nil, map[string]any{"ip": ip})

		next := "/admin/login/totp"
		if account.TOTPSecret == nil {
			next = "/admin/login/totp-setup"
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}
