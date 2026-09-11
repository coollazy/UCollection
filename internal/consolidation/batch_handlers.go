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

// createFlowHandler implements POST /admin/consolidation/prepare-flow — the
// merged「準備歸集」submit on consolidation_pending.html (ADR-0017: 補 TRX
// 手續費與 USDT 歸集整合為單一流程). It creates BOTH batches the integrated
// sign page needs (one consolidation_batches row for the USDT drains, one
// fee_topup_batches row for the TRX top-ups) from a single form submission,
// automatically computes the first/repeat TRX top-up amounts itself (no
// manual「每筆金額」input — 2026-09-11使用者拍板，見docs/進度.md), then hands off
// to the combined sign page.
//
// TOTP note (2026-09-11): this route deliberately does NOT require
// RequireTOTPCode — it only creates batch bookkeeping rows (destination/
// fee-source addresses, no funds move). The actual step-up requirement is
// consolidated entirely at the point signing/broadcasting happens: the
// combined sign page's own totp-code-input, consumed by the first
// /tron-proxy/{consolidation,fee-topup}/prepare call (RequireTOTPCodeOrRecentStepUp).
// This means GET /admin/consolidation/sign is also plain RequireSession now
// (see routes.go) — if it still required a fresh step-up, the operator would
// hit the reverify-totp interstitial as an unwanted THIRD prompt between
// this route and the sign page's own TOTP field, which is exactly the
// friction this change removes. An attacker holding only a stolen session
// cookie (no 2FA device) can still not move any funds: every prepare/
// broadcast call remains gated, and page.js's on-page item table lets the
// operator visually confirm destination/fee-source addresses (安全鐵律4的
// 自我核對延伸) before they ever type their master-wallet mnemonic + a valid
// TOTP code.
//
// Atomicity note: the two batch inserts are NOT wrapped in a single DB
// transaction. CreateConsolidationBatch/CreateFeeTopupBatch each write via
// deps.Pool directly and don't accept an external querier; rather than
// refactor both (and everything else that calls them) to thread a tx, this
// deliberately accepts non-atomic creation. All input validation that can
// fail (amount, order_ids) runs BEFORE any insert, and the consolidation
// batch is created first: if the fee-topup insert then fails (e.g. a
// malformed fee_source_address), the already-created consolidation_batches
// row is a harmless empty shell — it has no consolidation_items, moves no
// funds, and is simply never navigated to (the operator just re-submits).
// No partial-failure state can put money at risk, so a transaction buys
// nothing here.
func createFlowHandler(deps Deps) http.HandlerFunc {
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
		feeSource := strings.TrimSpace(r.FormValue("fee_source"))
		feeSourceAddress := strings.TrimSpace(r.FormValue("fee_source_address"))

		if !isValidTronAddress(destinationAddress) {
			http.Error(w, "invalid destination address format", http.StatusBadRequest)
			return
		}
		orderIDs, err := parseOrderIDs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 每筆補款金額改由伺服器自動試算、直接帶去簽名頁——不再要求商戶在本頁手動
		// 輸入/抄一次試算結果（2026-09-11使用者拍板，見docs/進度.md與ADR-0017）。
		// 在建立任何batch列之前先算，估算失敗（例如TronGrid暫時不可用）就不留下
		// 空batch。首筆/其餘筆金額分開算（精省手續費，ADR-0017決策2），送出前
		// 都已確定，不需要簽名頁再讓商戶臨時輸入。
		firstAmountSun, repeatAmountSun, _, _, _, err := estimateConsolidationFeesSun(ctx, deps, masterWalletID, destinationAddress, orderIDs[0])
		if err != nil {
			log.Printf("consolidation: create flow (fee estimate): %v", err)
			http.Error(w, "fee estimate failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		consolidationBatchID, err := CreateConsolidationBatch(ctx, deps, masterWalletID, destinationAddress)
		if err != nil {
			if errors.Is(err, errInvalidAddressFormat) {
				http.Error(w, "invalid destination address format", http.StatusBadRequest)
				return
			}
			log.Printf("consolidation: create flow (consolidation batch): %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		feeTopupBatchID, err := CreateFeeTopupBatch(ctx, deps, masterWalletID, feeSource, feeSourceAddress)
		if err != nil {
			switch {
			case errors.Is(err, errInvalidFeeSource):
				http.Error(w, "fee_source must be A1 or A2", http.StatusBadRequest)
			case errors.Is(err, errInvalidAddressFormat):
				http.Error(w, "invalid fee_source_address format", http.StatusBadRequest)
			default:
				log.Printf("consolidation: create flow (fee-topup batch): %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}

		targetType := "consolidation_batch"
		_ = audit.Log(ctx, deps.Pool, "admin", "CONSOLIDATION_FLOW_CREATED", &targetType, &consolidationBatchID, map[string]any{
			"master_wallet_id":       masterWalletID,
			"consolidation_batch_id": consolidationBatchID,
			"fee_topup_batch_id":     feeTopupBatchID,
			"destination_address":    destinationAddress,
			"fee_source":             feeSource,
			"fee_source_address":     feeSourceAddress,
			"order_ids":              orderIDs,
			"first_amount_sun":       firstAmountSun,
			"repeat_amount_sun":      repeatAmountSun,
		})

		q := url.Values{}
		q.Set("type", "combined")
		q.Set("consolidation_batch_id", strconv.FormatInt(consolidationBatchID, 10))
		q.Set("fee_topup_batch_id", strconv.FormatInt(feeTopupBatchID, 10))
		for _, id := range orderIDs {
			q.Add("order_id", strconv.FormatInt(id, 10))
		}
		q.Set("first_amount_per_order", strconv.FormatInt(firstAmountSun, 10))
		q.Set("repeat_amount_per_order", strconv.FormatInt(repeatAmountSun, 10))
		http.Redirect(w, r, "/admin/consolidation/sign?"+q.Encode(), http.StatusSeeOther)
	}
}
