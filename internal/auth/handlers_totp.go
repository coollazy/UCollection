package auth

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

type totpSetupPageData struct {
	QRCodeBase64 string
	Secret       string
	Error        string
}

type totpVerifyPageData struct {
	Error string
}

// totpSetupPageHandler implements 技術架構設計第9節步驟2 (GET half): first
// login, no totp_secret yet — show the QR/manual code. If the account
// already has a totp_secret by the time this loads (another pending_2fa
// session won the setup race first), send this session to the regular
// verify page instead — it must not keep offering a setup form for a
// secret that can never be written.
func totpSetupPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sess, err := loadPendingSession(ctx, deps, r)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if account.TOTPSecret != nil {
			http.Redirect(w, r, "/admin/login/totp?reason=totp_setup_raced", http.StatusSeeOther)
			return
		}

		secret := sess.PendingTOTPSecret
		if secret == nil {
			generated, err := newTOTPSecret(account.Username)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if err := setPendingTOTPSecret(ctx, deps.Pool, sess.ID, generated); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			secret = &generated
		}

		renderTOTPSetupPage(w, account.Username, *secret, "")
	}
}

func renderTOTPSetupPage(w http.ResponseWriter, accountName, secret, errMsg string) {
	png, err := totpQRCodePNG(accountName, secret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, http.StatusOK, "totp_setup.html", totpSetupPageData{
		QRCodeBase64: base64.StdEncoding.EncodeToString(png),
		Secret:       secret,
		Error:        errMsg,
	})
}

// totpSetupSubmitHandler implements 技術架構設計第9節步驟2 (POST half),
// including the "UPDATE ... WHERE totp_secret IS NULL" race handling
// described there verbatim.
func totpSetupSubmitHandler(deps Deps, limiter *ipLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !limiter.allow(ip) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}

		ctx := r.Context()
		sess, err := loadPendingSession(ctx, deps, r)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if account.TOTPSecret != nil {
			http.Redirect(w, r, "/admin/login/totp?reason=totp_setup_raced", http.StatusSeeOther)
			return
		}
		if sess.PendingTOTPSecret == nil {
			// GET always persists a secret before showing the form; a POST
			// without one means the session skipped straight to POST somehow.
			http.Redirect(w, r, "/admin/login/totp-setup", http.StatusSeeOther)
			return
		}
		secret := *sess.PendingTOTPSecret

		if err := r.ParseForm(); err != nil {
			renderTOTPSetupPage(w, account.Username, secret, "表單格式錯誤")
			return
		}
		code := r.PostFormValue("code")

		step, ok := verifyTOTPCode(secret, code, time.Now())
		if !ok {
			locked, err := handleTOTPFailure(ctx, deps, limiter, ip, sess, "TOTP_SETUP_FAILED")
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if locked {
				clearSessionCookie(w, deps.CookieSecure)
				http.Redirect(w, r, "/admin/login?reason=locked", http.StatusSeeOther)
				return
			}
			renderTOTPSetupPage(w, account.Username, secret, "驗證碼錯誤")
			return
		}
		if isReplay(account, step) {
			renderTOTPSetupPage(w, account.Username, secret, "此驗證碼已被使用，請等待新一組驗證碼後再試")
			return
		}

		updated, err := setTOTPSecretIfUnset(ctx, deps.Pool, account.ID, secret)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !updated {
			http.Redirect(w, r, "/admin/login/totp?reason=totp_setup_raced", http.StatusSeeOther)
			return
		}
		account.TOTPSecret = &secret

		if err := completeTOTPStep(ctx, deps, account, sess, step); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "TOTP_SETUP_SUCCESS", nil, nil, map[string]any{"ip": ip})

		if token, ok := sessionTokenFromRequest(r); ok {
			setSessionCookie(w, token, deps.CookieSecure, activeSessionTTL)
		}
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
	}
}

// totpVerifyPageHandler implements 技術架構設計第9節步驟3 (GET half): regular
// login, totp_secret already set.
func totpVerifyPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, err := loadPendingSession(ctx, deps, r); err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if account.TOTPSecret == nil {
			http.Redirect(w, r, "/admin/login/totp-setup", http.StatusSeeOther)
			return
		}

		render(w, http.StatusOK, "totp_verify.html", totpVerifyPageData{Error: reasonMessage(r.URL.Query().Get("reason"))})
	}
}

func totpVerifySubmitHandler(deps Deps, limiter *ipLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !limiter.allow(ip) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}

		ctx := r.Context()
		sess, err := loadPendingSession(ctx, deps, r)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if account.TOTPSecret == nil {
			http.Redirect(w, r, "/admin/login/totp-setup", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			render(w, http.StatusBadRequest, "totp_verify.html", totpVerifyPageData{Error: "表單格式錯誤"})
			return
		}
		code := r.PostFormValue("code")

		step, ok := verifyTOTPCode(*account.TOTPSecret, code, time.Now())
		if !ok {
			locked, err := handleTOTPFailure(ctx, deps, limiter, ip, sess, "TOTP_VERIFY_FAILED")
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if locked {
				clearSessionCookie(w, deps.CookieSecure)
				http.Redirect(w, r, "/admin/login?reason=locked", http.StatusSeeOther)
				return
			}
			render(w, http.StatusUnauthorized, "totp_verify.html", totpVerifyPageData{Error: "驗證碼錯誤"})
			return
		}
		if isReplay(account, step) {
			render(w, http.StatusUnauthorized, "totp_verify.html", totpVerifyPageData{Error: "此驗證碼已被使用，請等待新一組驗證碼後再試"})
			return
		}

		if err := completeTOTPStep(ctx, deps, account, sess, step); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "TOTP_VERIFY_SUCCESS", nil, nil, map[string]any{"ip": ip})

		if token, ok := sessionTokenFromRequest(r); ok {
			setSessionCookie(w, token, deps.CookieSecure, activeSessionTTL)
		}
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
	}
}
