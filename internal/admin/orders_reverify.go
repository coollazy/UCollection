package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/scanner"
)

// reverifyHandler implements POST /admin/orders/{id}/reverify (技術架構設計第
// 11節「手動重新檢查此地址」). 無論成功或失敗都要記錄 audit_logs（ORDER_MANUAL_
// REVERIFY，第11節「稽核日誌查詢」段落：「即使僅掛RequireSession，重新查證會寫入
// incoming_transactions，皆屬記錄商戶所有操作歷程字面要求涵蓋的操作」）。
func reverifyHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid order id", http.StatusBadRequest)
			return
		}

		ord, err := order.GetByID(ctx, deps.Pool, id)
		if errors.Is(err, order.ErrOrderNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		targetType := "order"
		reverifyErr := scanner.ReverifyOrder(ctx, deps.scannerDeps(), ord)

		detail := map[string]any{"address": ord.Address}
		if reverifyErr != nil {
			detail["error"] = reverifyErr.Error()
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "ORDER_MANUAL_REVERIFY", &targetType, &ord.ID, detail)

		if reverifyErr != nil {
			http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash_error=reverify_failed", id), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/admin/orders/%d?flash=reverify_ok", id), http.StatusSeeOther)
	}
}
