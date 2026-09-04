package consolidation

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

type prepareResponse struct {
	ItemID      int64           `json:"item_id"`
	TxID        string          `json:"tx_id"`
	Transaction json.RawMessage `json:"transaction"`
}

type prepareConsolidationRequest struct {
	BatchID int64 `json:"batch_id"`
	OrderID int64 `json:"order_id"`
}

// prepareConsolidationHandler implements POST /tron-proxy/consolidation/
// prepare (技術架構設計第10節「組交易與廣播」步驟1): re-queries the live on-chain
// USDT balance (never trusts a stale value from the list page), builds an
// unsigned TriggerSmartContract transfer() call for the FULL balance, and
// records a new consolidation_items row in 'pending' status.
func prepareConsolidationHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req prepareConsolidationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		batch, err := loadConsolidationBatch(ctx, deps.Pool, req.BatchID)
		if errors.Is(err, errRowNotFound) {
			writeError(w, errAPIBatchNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}

		ord, err := order.GetByID(ctx, deps.Pool, req.OrderID)
		if errors.Is(err, order.ErrOrderNotFound) {
			writeError(w, errAPIOrderNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}
		if ord.MasterWalletID != batch.MasterWalletID {
			writeError(w, errAPIBatchOrderMismatch)
			return
		}
		if ord.ConsolidationStatus != order.ConsolidationNotConsolidated {
			writeError(w, errAPIAlreadyConsolidated)
			return
		}

		balance, err := deps.TronClient.TRC20Balance(ctx, ord.Address, deps.USDTContractAddress)
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}
		if balance <= 0 {
			writeError(w, errAPINoBalance)
			return
		}

		parameter, err := abiEncodeTransfer(batch.DestinationAddress, balance)
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}

		prepared, err := deps.TronClient.TriggerSmartContract(ctx, tronclient.TriggerSmartContractParams{
			OwnerAddress:     ord.Address,
			ContractAddress:  deps.USDTContractAddress,
			FunctionSelector: "transfer(address,uint256)",
			Parameter:        parameter,
			CallValue:        0,
		})
		if err != nil {
			writeError(w, newAPIError(http.StatusBadGateway, "TRONGRID_PREPARE_FAILED", err.Error()))
			return
		}

		var itemID int64
		err = deps.Pool.QueryRow(ctx, `
			INSERT INTO consolidation_items (batch_id, order_id, amount, tx_hash, broadcast_status)
			VALUES ($1, $2, $3, $4, 'pending')
			RETURNING id
		`, batch.ID, ord.ID, balance, prepared.TxID).Scan(&itemID)
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}

		targetType := "consolidation_item"
		_ = audit.Log(ctx, deps.Pool, "admin", "CONSOLIDATION", &targetType, &itemID, map[string]any{
			"stage": "prepared", "batch_id": batch.ID, "order_id": ord.ID, "amount": balance, "tx_hash": prepared.TxID,
		})

		writeJSON(w, http.StatusOK, prepareResponse{ItemID: itemID, TxID: prepared.TxID, Transaction: prepared.Transaction})
	}
}

type prepareFeeTopupRequest struct {
	BatchID int64 `json:"batch_id"`
	OrderID int64 `json:"order_id"`
	Amount  int64 `json:"amount"`
}

// prepareFeeTopupHandler implements POST /tron-proxy/fee-topup/prepare.
// Unlike consolidation, the amount is operator-specified (技術架構設計第10節
// 「轉入金額由商戶手動輸入」), not derived from a balance query, and there is
// deliberately no check against the order's consolidation_status — TRX
// top-up and USDT consolidation are independent batches with no ordering
// enforced between them (技術架構設計第10節「TRX儲值與USDT歸集的時序依賴，系統不做順
// 序把關」).
func prepareFeeTopupHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req prepareFeeTopupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		if req.Amount <= 0 {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		batch, err := loadFeeTopupBatch(ctx, deps.Pool, req.BatchID)
		if errors.Is(err, errRowNotFound) {
			writeError(w, errAPIBatchNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}

		ord, err := order.GetByID(ctx, deps.Pool, req.OrderID)
		if errors.Is(err, order.ErrOrderNotFound) {
			writeError(w, errAPIOrderNotFound)
			return
		}
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}
		if ord.MasterWalletID != batch.MasterWalletID {
			writeError(w, errAPIBatchOrderMismatch)
			return
		}

		prepared, err := deps.TronClient.CreateTransaction(ctx, tronclient.CreateTransactionParams{
			OwnerAddress: batch.FeeSourceAddress,
			ToAddress:    ord.Address,
			Amount:       req.Amount,
		})
		if err != nil {
			writeError(w, newAPIError(http.StatusBadGateway, "TRONGRID_PREPARE_FAILED", err.Error()))
			return
		}

		var itemID int64
		err = deps.Pool.QueryRow(ctx, `
			INSERT INTO fee_topup_items (batch_id, order_id, amount, tx_hash, broadcast_status)
			VALUES ($1, $2, $3, $4, 'pending')
			RETURNING id
		`, batch.ID, ord.ID, req.Amount, prepared.TxID).Scan(&itemID)
		if err != nil {
			writeError(w, errAPIInternal)
			return
		}

		targetType := "fee_topup_item"
		_ = audit.Log(ctx, deps.Pool, "admin", "FEE_TOPUP", &targetType, &itemID, map[string]any{
			"stage": "prepared", "batch_id": batch.ID, "order_id": ord.ID, "amount": req.Amount, "tx_hash": prepared.TxID,
		})

		writeJSON(w, http.StatusOK, prepareResponse{ItemID: itemID, TxID: prepared.TxID, Transaction: prepared.Transaction})
	}
}
