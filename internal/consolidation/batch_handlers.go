package consolidation

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coollazy/UCollection/internal/audit"
)

// parseOrderIDs reads the repeated "order_id" checkbox values from a
// submitted form. Returns an error if none were checked or any value isn't
// a valid integer.
func parseOrderIDs(r *http.Request) ([]int64, error) {
	raw := r.Form["order_id"]
	if len(raw) == 0 {
		return nil, errors.New("at least one order must be selected")
	}
	ids := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, errors.New("invalid order_id")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// parseTRXAmount converts an operator-entered TRX decimal string into int64
// 最小單位 (sun, 6 decimals). Mirrors internal/admin.parseUSDTAmount: pure
// string handling, zero floating-point (安全鐵律6) — TRX's sun and USDT-TRC20
// share 6-decimal precision, so the logic is identical. Kept package-local
// (like formatMicroAmount's admin/consolidation duplication) rather than
// crossing the admin package boundary.
func parseTRXAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("每筆金額為必填")
	}
	if strings.HasPrefix(s, "-") {
		return 0, errors.New("金額不可為負數")
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if hasFrac && len(fracPart) > 6 {
		return 0, errors.New("最多只能輸入6位小數")
	}
	fracPart += strings.Repeat("0", 6-len(fracPart))
	if intPart == "" {
		intPart = "0"
	}

	sun, err := strconv.ParseInt(intPart+fracPart, 10, 64)
	if err != nil {
		return 0, errors.New("無法辨識的金額格式")
	}
	if sun <= 0 {
		return 0, errors.New("金額必須大於0")
	}
	return sun, nil
}

// signRedirectURL builds the 303 target for the sign page, carrying the
// batch this handler just created plus the order IDs the operator checked
// — consolidation_batches/fee_topup_batches deliberately don't persist a
// selected-order list (items are only created one at a time, at prepare
// time, per 技術架構設計第10節「組交易與廣播」步驟1), so this is the only place that
// list exists after this request. Order IDs aren't sensitive so carrying
// them in the URL is fine; the destination/fee-source address is NOT
// repeated here — the sign page loads that from the batch row itself
// (authoritative DB state), not from a re-editable query string.
func signRedirectURL(kind string, batchID int64, orderIDs []int64) string {
	q := url.Values{}
	q.Set("type", kind)
	q.Set("batch_id", strconv.FormatInt(batchID, 10))
	for _, id := range orderIDs {
		q.Add("order_id", strconv.FormatInt(id, 10))
	}
	return "/admin/consolidation/sign?" + q.Encode()
}

// createConsolidationBatchHandler implements POST /admin/consolidation/
// batches (checkboxes on consolidation_pending.html): validates the
// destination address, opens a new consolidation_batches row, and hands off
// to the sign page with the selected orders.
func createConsolidationBatchHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		masterWalletID, err := strconv.ParseInt(r.FormValue("master_wallet_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid master_wallet_id", http.StatusBadRequest)
			return
		}
		if _, err := getMasterWallet(ctx, deps.Pool, masterWalletID); err != nil {
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "master wallet not found", http.StatusBadRequest)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		destinationAddress := strings.TrimSpace(r.FormValue("destination_address"))
		orderIDs, err := parseOrderIDs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		batchID, err := CreateConsolidationBatch(ctx, deps, masterWalletID, destinationAddress)
		if err != nil {
			if errors.Is(err, errInvalidAddressFormat) {
				http.Error(w, "invalid destination address format", http.StatusBadRequest)
				return
			}
			log.Printf("consolidation: create batch: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// 階段07全系統審查發現：本操作是資金轉出流程的起手式（本身已疊加
		// RequireFreshTOTP），但先前完全沒有稽核紀錄——只有後續prepare/broadcast
		// 兩階段有。補上以涵蓋完整的操作軌跡。
		targetType := "consolidation_batch"
		_ = audit.Log(ctx, deps.Pool, "admin", "CONSOLIDATION_BATCH_CREATED", &targetType, &batchID, map[string]any{
			"master_wallet_id":    masterWalletID,
			"destination_address": destinationAddress,
			"order_ids":           orderIDs,
		})

		http.Redirect(w, r, signRedirectURL("consolidation", batchID, orderIDs), http.StatusSeeOther)
	}
}

// createFeeTopupBatchHandler implements POST /admin/consolidation/
// fee-topup-batches. amount_per_order is only a default the sign page
// pre-fills per item — the amount actually broadcast is whatever value
// each /tron-proxy/fee-topup/prepare call carries (技術架構設計第10節「可對批次內
// 所有勾選地址套用同一數值，或個別調整」), so it isn't stored on the batch row.
func createFeeTopupBatchHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		masterWalletID, err := strconv.ParseInt(r.FormValue("master_wallet_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid master_wallet_id", http.StatusBadRequest)
			return
		}
		if _, err := getMasterWallet(ctx, deps.Pool, masterWalletID); err != nil {
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "master wallet not found", http.StatusBadRequest)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		feeSource := strings.TrimSpace(r.FormValue("fee_source"))
		feeSourceAddress := strings.TrimSpace(r.FormValue("fee_source_address"))

		amountPerOrder, err := parseTRXAmount(r.FormValue("amount_per_order"))
		if err != nil {
			http.Error(w, "invalid amount_per_order: "+err.Error(), http.StatusBadRequest)
			return
		}

		orderIDs, err := parseOrderIDs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		batchID, err := CreateFeeTopupBatch(ctx, deps, masterWalletID, feeSource, feeSourceAddress)
		if err != nil {
			switch {
			case errors.Is(err, errInvalidFeeSource):
				http.Error(w, "fee_source must be A1 or A2", http.StatusBadRequest)
			case errors.Is(err, errInvalidAddressFormat):
				http.Error(w, "invalid fee_source_address format", http.StatusBadRequest)
			default:
				log.Printf("consolidation: create fee-topup batch: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}

		targetType := "fee_topup_batch"
		_ = audit.Log(ctx, deps.Pool, "admin", "FEE_TOPUP_BATCH_CREATED", &targetType, &batchID, map[string]any{
			"master_wallet_id":   masterWalletID,
			"fee_source":         feeSource,
			"fee_source_address": feeSourceAddress,
			"order_ids":          orderIDs,
			"amount_per_order":   amountPerOrder,
		})

		q := url.Values{}
		q.Set("amount_per_order", strconv.FormatInt(amountPerOrder, 10))
		http.Redirect(w, r, signRedirectURL("fee-topup", batchID, orderIDs)+"&"+q.Encode(), http.StatusSeeOther)
	}
}
