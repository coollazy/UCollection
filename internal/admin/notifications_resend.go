package admin

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/webhook"
)

// resendNotificationHandler implements POST /admin/notifications/{id}/resend
// (技術架構設計第8節「手動重發」＋第11節：呼叫已定案的手動重發邏輯，不受狀態限制、不
// 影響attempt_count). Calls webhook.Resend directly — no adapter needed, its
// doc comment says it was exported specifically for admin to call this way.
// RequireSession only (技術架構設計路由表原文): resending an already-computed,
// already-audited delivery doesn't create any new business judgment the way
// override-status does, so it isn't in the RequireFreshTOTP tier.
func resendNotificationHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Redirect(w, r, "/admin/notifications?flash_error="+url.QueryEscape("找不到指定的通知記錄"), http.StatusSeeOther)
			return
		}

		resendErr := webhook.Resend(ctx, deps.Pool, deps.HTTPClient, id)

		targetType := "webhook_delivery"
		detail := map[string]any{}
		if resendErr != nil {
			detail["error"] = resendErr.Error()
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "NOTIFICATION_MANUAL_RESEND", &targetType, &id, detail)

		if resendErr != nil {
			http.Redirect(w, r, "/admin/notifications?flash_error="+url.QueryEscape("重發失敗："+resendErr.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/notifications?flash="+url.QueryEscape("已重發"), http.StatusSeeOther)
	}
}
