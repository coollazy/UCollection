package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

// appendReverifiedFlag adds a marker query param so the page RequireFreshTOTP
// sent the operator back to (見middleware.go之RequireFreshTOTP doc comment)
// can show a "請重新送出剛才的操作" notice — the original POST body was lost
// when the step-up redirect fired, so this is purely informational.
func appendReverifiedFlag(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "totp_reverified=1"
}

type reverifyPageData struct {
	Next  string
	Error string
}

// sanitizeNextPath guards against open-redirect via a crafted
// "?next=https://evil.example" or "?next=//evil.example" query value —
// next must be a same-app relative path, otherwise fall back to the
// dashboard. RequireFreshTOTP itself only ever builds next from
// r.URL.RequestURI() (always relative), but this endpoint also accepts a
// POSTed next value directly from the form, so it's re-validated here too.
func sanitizeNextPath(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/admin/dashboard"
	}
	return next
}

// reverifyPageHandler/reverifySubmitHandler implement 技術架構設計第9節「高風險
// 操作Step-up驗證」. Registered behind RequireSession only (not
// RequireFreshTOTP — that would redirect back to itself): the operator is
// already fully logged in, they just need to prove a recent TOTP code
// before RequireFreshTOTP-gated routes will let them through.
func reverifyPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, http.StatusOK, "reverify.html", reverifyPageData{
			Next: sanitizeNextPath(r.URL.Query().Get("next")),
		})
	}
}

func reverifySubmitHandler(deps Deps, limiter *ipLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !limiter.allow(ip) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}

		sess, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "internal error: reverify used without RequireSession", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if account.TOTPSecret == nil {
			// Can't happen: reaching an active session already implies 2FA
			// setup completed (technical架構設計第9節步驟3 is the only path to
			// 'active'). Fail loudly rather than silently mis-redirecting.
			http.Error(w, "internal error: active session with no totp_secret", http.StatusInternalServerError)
			return
		}

		if err := r.ParseForm(); err != nil {
			render(w, http.StatusBadRequest, "reverify.html", reverifyPageData{Next: "/admin/dashboard", Error: "表單格式錯誤"})
			return
		}
		next := sanitizeNextPath(r.PostFormValue("next"))
		code := r.PostFormValue("code")

		step, ok := verifyTOTPCode(*account.TOTPSecret, code, time.Now())
		if !ok {
			locked, err := handleTOTPFailure(ctx, deps, limiter, ip, sess, "TOTP_REVERIFY_FAILED")
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if locked {
				clearSessionCookie(w, deps.CookieSecure)
				http.Redirect(w, r, "/admin/login?reason=locked", http.StatusSeeOther)
				return
			}
			render(w, http.StatusUnauthorized, "reverify.html", reverifyPageData{Next: next, Error: "驗證碼錯誤"})
			return
		}
		if isReplay(account, step) {
			render(w, http.StatusUnauthorized, "reverify.html", reverifyPageData{Next: next, Error: "此驗證碼已被使用，請等待新一組驗證碼後再試"})
			return
		}

		if err := completeReverify(ctx, deps, account, sess, step); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "TOTP_REVERIFY_SUCCESS", nil, nil, map[string]any{"ip": ip})

		http.Redirect(w, r, appendReverifiedFlag(next), http.StatusSeeOther) //nolint:gosec // next is always sanitizeNextPath()'d above (must start with a single "/"), so this can't become an open redirect
	}
}
