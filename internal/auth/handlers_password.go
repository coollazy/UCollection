package auth

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
)

type passwordPageData struct {
	Error  string
	Notice string
}

func passwordPageHandler(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, http.StatusOK, "password.html", passwordPageData{Notice: totpReverifiedNotice(r)})
	}
}

// totpReverifiedNotice mirrors internal/admin's helper of the same name
// (見該package templates.go——這裡故意重複一份小helper而非跨package共用，比照本
// 專案既有慣例，見internal/consolidation對CSP常數的處理). Tells the operator why
// they landed back on this GET page instead of their original POST
// completing — see RequireTOTPCode's doc comment.
func totpReverifiedNotice(r *http.Request) string {
	switch r.URL.Query().Get("totp_error") {
	case "missing":
		return "此操作需要輸入TOTP驗證碼，請重新填寫並送出"
	case "invalid":
		return "TOTP驗證碼錯誤，請重新填寫並送出"
	case "replay":
		return "此驗證碼已被使用，請等待新一組驗證碼後再試"
	}
	if r.URL.Query().Get("totp_reverified") != "1" {
		return ""
	}
	return "TOTP已重新驗證，請重新填寫並送出剛才的操作"
}

// passwordSubmitHandler implements 技術架構設計第9節「修改密碼」: current
// password re-checked even though the request already passed
// RequireSession+RequireFreshTOTP (a stolen session cookie without the
// password shouldn't be enough to lock the real operator out), then every
// session is deleted on success (含發起變更的當前session) forcing a full
// re-login including TOTP.
func passwordSubmitHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := r.ParseForm(); err != nil {
			render(w, http.StatusBadRequest, "password.html", passwordPageData{Error: "表單格式錯誤"})
			return
		}
		current := r.PostFormValue("current_password")
		newPassword := r.PostFormValue("new_password")

		account, err := loadAccount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if !checkPassword(account.PasswordHash, current) {
			render(w, http.StatusUnauthorized, "password.html", passwordPageData{Error: "目前密碼不正確"})
			return
		}
		if err := validatePasswordLength(newPassword); err != nil {
			render(w, http.StatusBadRequest, "password.html", passwordPageData{Error: "新密碼長度至少需12碼"})
			return
		}

		hash, err := hashPassword(newPassword)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := updatePassword(ctx, deps.Pool, account.ID, hash); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := deleteAllSessions(ctx, deps.Pool); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "PASSWORD_CHANGED", nil, nil, nil)

		clearSessionCookie(w, deps.CookieSecure)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}
