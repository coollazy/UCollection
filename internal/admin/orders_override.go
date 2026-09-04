package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/order"
)

// overrideStatusHandler implements POST /admin/orders/{id}/override-status
// (技術架構設計第11節「手動改判」). order.ManualTransition already does the
// whole thing atomically (白名單檢查、order_state_transitions.note寫入、終態時
// 同transaction INSERT webhook_deliveries) — this handler's only extra
// responsibility is translating its two "not allowed" error shapes into the
// ILLEGAL_STATE_TRANSITION audit entry 技術架構設計第5節 requires (order.go
// deliberately doesn't depend on internal/audit — see this Part's plan
// notes), which is not written anywhere else in the codebase yet.
func overrideStatusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid order id", http.StatusBadRequest)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		toStatus := order.Status(r.PostForm.Get("to_status"))
		note := r.PostForm.Get("note")
		if note == "" {
			http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash_error=note_required", id), http.StatusSeeOther)
			return
		}

		err = order.ManualTransition(ctx, deps.Pool, id, toStatus, "admin", note)

		targetType := "order"
		if errors.Is(err, order.ErrManualTransitionNotAllowed) || errors.Is(err, order.ErrInvalidTransition) {
			_ = audit.Log(ctx, deps.Pool, "admin", "ILLEGAL_STATE_TRANSITION", &targetType, &id, map[string]any{
				"to_status": string(toStatus),
				"note":      note,
			})
			http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash_error=transition_not_allowed", id), http.StatusSeeOther)
			return
		}
		if errors.Is(err, order.ErrOrderNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		_ = audit.Log(ctx, deps.Pool, "admin", "ORDER_MANUAL_OVERRIDE", &targetType, &id, map[string]any{
			"to_status": string(toStatus),
			"note":      note,
		})
		http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash=override_ok", id), http.StatusSeeOther)
	}
}
