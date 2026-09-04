package auth

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
)

type passwordPageData struct {
	Error string
}

func passwordPageHandler(_ Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		render(w, http.StatusOK, "password.html", passwordPageData{})
	}
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
