package consolidation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

type broadcastRequest struct {
	ItemID      int64           `json:"item_id"`
	Transaction json.RawMessage `json:"transaction"`
}

type broadcastResponse struct {
	Status      string `json:"status"` // "success" | "failed" | "broadcasting" (unknown — see decideBroadcastOutcome)
	TxID        string `json:"tx_id"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

// decideBroadcastOutcome maps one BroadcastTransaction call into the
// three-way outcome from 技術架構設計第10節「廣播結果判斷」:
//
//   - networkFailure=true (status stays "broadcasting"): the HTTP round-trip
//     itself failed, so we genuinely do not know whether TronGrid received
//     the transaction (CLAUDE.md 安全鐵律7, 驗證結論-06 觀察4's real repeat-
//     broadcast incident). The item is left in 'broadcasting' for the lazy
//     reconcile pass (reconcile.go) to resolve later via TransactionInfo —
//     it must NOT be marked failed, which would invite an unsafe retry.
//   - status="failed": TronGrid DID respond, either with an explicit
//     rejection (result:false) or a response we couldn't parse. Either way
//     we know for certain nothing was accepted, so this is a real, final
//     failure — re-preparing (a fresh item, fresh txID) is safe.
//   - status="success": TronGrid explicitly accepted the broadcast.
func decideBroadcastOutcome(result tronclient.BroadcastResult, err error) (status, errorDetail string, networkFailure bool) {
	if errors.Is(err, tronclient.ErrBroadcastNetworkFailure) {
		return "broadcasting", "", true
	}
	if err != nil {
		return "failed", err.Error(), false
	}
	if result.Success {
		return "success", "", false
	}
	detail := result.Message
	if result.Code != "" {
		if detail != "" {
			detail = result.Code + ": " + detail
		} else {
			detail = result.Code
		}
	}
	return "failed", detail, false
}

// broadcastConsolidationHandler implements POST /tron-proxy/consolidation/
// broadcast (技術架構設計第10節「組交易與廣播」步驟5). item.broadcast_status must
// still be 'pending' — this is what prevents the same item from ever being
// broadcast twice (技術架構設計第10節「從不改寫既有列...重試會產生第二筆」: retries go
// through a brand new prepare call, never a second broadcast of the same
// item).
func broadcastConsolidationHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req broadcastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		item, err := loadConsolidationItem(ctx, deps.Pool, req.ItemID)
		if errors.Is(err, errRowNotFound) {
			writeError(w, errAPIItemNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}
		if item.BroadcastStatus != "pending" {
			writeError(w, errAPIItemNotPending)
			return
		}

		gotTxID, err := extractTxIDFromJSON(req.Transaction)
		if err != nil || gotTxID == "" || gotTxID != item.TxHash {
			writeError(w, errAPITxIDMismatch)
			return
		}

		if _, err := deps.Pool.Exec(ctx, `UPDATE consolidation_items SET broadcast_status = 'broadcasting', updated_at = now() WHERE id = $1`, item.ID); err != nil {
			writeError(w, errAPIInternal)
			return
		}

		result, broadcastErr := deps.TronClient.BroadcastTransaction(ctx, req.Transaction)
		status, errorDetail, _ := decideBroadcastOutcome(result, broadcastErr)

		if _, err := deps.Pool.Exec(ctx, `
			UPDATE consolidation_items SET broadcast_status = $2, error_detail = $3, updated_at = now() WHERE id = $1
		`, item.ID, status, nullIfEmpty(errorDetail)); err != nil {
			writeError(w, errAPIInternal)
			return
		}

		if status == "success" {
			if err := order.MarkConsolidated(ctx, deps.Pool, item.OrderID); err != nil {
				writeError(w, errAPIInternal)
				return
			}
			// Best-effort per 技術架構設計第10節: a full address book must never
			// affect a consolidation that has already succeeded on chain.
			_ = AutoSaveIfNew(ctx, deps, item.DestinationAddress)
		}

		targetType := "consolidation_item"
		_ = audit.Log(ctx, deps.Pool, "admin", "CONSOLIDATION", &targetType, &item.ID, map[string]any{
			"stage": "broadcast", "status": status, "order_id": item.OrderID, "tx_hash": item.TxHash, "error_detail": errorDetail,
		})

		writeJSON(w, http.StatusOK, broadcastResponse{Status: status, TxID: item.TxHash, ErrorDetail: errorDetail})
	}
}

// broadcastFeeTopupHandler is broadcastConsolidationHandler's counterpart
// for TRX top-ups — same decideBroadcastOutcome logic, but success never
// touches orders.consolidation_status (topping up TRX is just a
// prerequisite step, not the consolidation itself) and there's no address
// book to auto-save to.
func broadcastFeeTopupHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req broadcastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		item, err := loadFeeTopupItem(ctx, deps.Pool, req.ItemID)
		if errors.Is(err, errRowNotFound) {
			writeError(w, errAPIItemNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}
		if item.BroadcastStatus != "pending" {
			writeError(w, errAPIItemNotPending)
			return
		}

		gotTxID, err := extractTxIDFromJSON(req.Transaction)
		if err != nil || gotTxID == "" || gotTxID != item.TxHash {
			writeError(w, errAPITxIDMismatch)
			return
		}

		if err := updateFeeTopupBroadcasting(ctx, deps, item.ID); err != nil {
			writeError(w, errAPIInternal)
			return
		}

		result, broadcastErr := deps.TronClient.BroadcastTransaction(ctx, req.Transaction)
		status, errorDetail, _ := decideBroadcastOutcome(result, broadcastErr)

		if _, err := deps.Pool.Exec(ctx, `
			UPDATE fee_topup_items SET broadcast_status = $2, error_detail = $3, updated_at = now() WHERE id = $1
		`, item.ID, status, nullIfEmpty(errorDetail)); err != nil {
			writeError(w, errAPIInternal)
			return
		}

		targetType := "fee_topup_item"
		_ = audit.Log(ctx, deps.Pool, "admin", "FEE_TOPUP", &targetType, &item.ID, map[string]any{
			"stage": "broadcast", "status": status, "order_id": item.OrderID, "tx_hash": item.TxHash, "error_detail": errorDetail,
		})

		writeJSON(w, http.StatusOK, broadcastResponse{Status: status, TxID: item.TxHash, ErrorDetail: errorDetail})
	}
}

func updateFeeTopupBroadcasting(ctx context.Context, deps Deps, itemID int64) error {
	_, err := deps.Pool.Exec(ctx, `UPDATE fee_topup_items SET broadcast_status = 'broadcasting', updated_at = now() WHERE id = $1`, itemID)
	return err
}
