package admin

import (
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/audit"
)

// masterWalletsReactivateErrorMessage maps a short "?flash_error="-style
// query code to Chinese text — same short-code-in-URL convention
// internal/consolidation's address book uses (見internal/consolidation/
// handlers_addressbook.go).
func masterWalletsReactivateErrorMessage(code string) string {
	switch code {
	case "already_active":
		return "此代收主錢包已是使用中，無需操作"
	case "not_found":
		return "找不到指定的代收主錢包"
	default:
		return ""
	}
}

// reactivateMasterWalletHandler implements POST /admin/master-wallets/{id}/
// reactivate (技術架構設計第11節「恢復使用中」): 沿用既有xpub記錄，同一transaction內
// 切換使用中/已停用狀態，last_derived_index維持原值續用。目標本身已是使用中時回傳
// 明確錯誤，不做靜默放行。
func reactivateMasterWalletHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			redirectMasterWalletsError(w, r, "not_found")
			return
		}

		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM master_wallets WHERE id = $1`, id).Scan(&status)
		if err != nil {
			redirectMasterWalletsError(w, r, "not_found")
			return
		}
		if status == "active" {
			redirectMasterWalletsError(w, r, "already_active")
			return
		}

		if _, err := tx.Exec(ctx, `UPDATE master_wallets SET status = 'inactive' WHERE status = 'active'`); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE master_wallets SET status = 'active' WHERE id = $1`, id); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		targetType := "master_wallet"
		_ = audit.Log(ctx, deps.Pool, "admin", "MASTER_WALLET_REACTIVATED", &targetType, &id, nil)

		http.Redirect(w, r, "/admin/master-wallets", http.StatusSeeOther)
	}
}

// redirectMasterWalletsError redirects with a short "?error=<code>" —
// the untranslated code, not the message text (see
// masterWalletsReactivateErrorMessage's doc comment: same short-code
// convention as internal/consolidation, translation happens where the
// redirect target reads the query string, not before the redirect).
func redirectMasterWalletsError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin/master-wallets?error="+code, http.StatusSeeOther)
}
