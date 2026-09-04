package auth

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
)

// logoutHandler implements 技術架構設計第9節「Session模型」logout: not wrapped
// with RequireSession — it just deletes whatever session the cookie
// points to, active or pending_2fa, so a user mid-two-stage-login can also
// bail out and start over.
func logoutHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if token, ok := sessionTokenFromRequest(r); ok {
			if sess, err := getSessionByToken(ctx, deps.Pool, token); err == nil {
				_ = deleteSession(ctx, deps.Pool, sess.ID)
				_ = audit.Log(ctx, deps.Pool, "admin", "LOGOUT", nil, nil, nil)
			}
		}
		clearSessionCookie(w, deps.CookieSecure)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}
