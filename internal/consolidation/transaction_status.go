package consolidation

import (
	"encoding/json"
	"net/http"
)

// transactionInfoRequest is POST /tron-proxy/consolidation/transaction-info's
// request body. Kind selects how "success" is judged (usdt = TRC20
// transfer() via triggersmartcontract, trx = a native TransferContract) —
// see transactionInfoHandler's doc comment for why the two need different
// judgment logic.
type transactionInfoRequest struct {
	TxID string `json:"tx_id"`
	Kind string `json:"kind"`
}

type transactionInfoResponse struct {
	Found   bool `json:"found"`
	Success bool `json:"success"`
}

// transactionInfoHandler implements POST /tron-proxy/consolidation/
// transaction-info: a read-only on-chain status lookup for a tx_id the sign
// page already broadcast, so the operator can poll "did this actually land
// yet" without ever re-broadcasting — CLAUDE.md 安全鐵律7 requires querying
// on-chain state (gettransactioninfobyid, here via
// deps.TronClient.TransactionInfo) rather than assuming a broadcast
// network-layer failure means the transaction never happened.
//
// Despite living under /tron-proxy/..., this route is deliberately
// RequireSession-only (no TOTP/step-up) — see routes.go's comment at this
// route's registration for why: it is read-only, it broadcasts nothing, and
// no funds ever move as a side effect of calling it.
//
// kind matters because a TRC20 transfer() and a native TRX transfer report
// success differently in gettransactioninfobyid's response: only
// TriggerSmartContract transactions get a receipt.result field
// ("SUCCESS"/"REVERT"/...) at all — a native TransferContract transaction
// has no receipt.result field whatsoever (見
// internal/tronclient.TransactionReceipt的doc comment，驗證結論-06 附加測試), so
// for kind="trx" success is judged purely by whether the transaction landed
// in a block (BlockNumber > 0) instead.
func transactionInfoHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req transactionInfoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		if req.TxID == "" || (req.Kind != "usdt" && req.Kind != "trx") {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		info, err := deps.TronClient.TransactionInfo(ctx, req.TxID)
		if err != nil {
			writeError(w, newAPIError(http.StatusBadGateway, "TRONGRID_TXINFO_FAILED", err.Error()))
			return
		}

		var success bool
		if req.Kind == "usdt" {
			success = info.Receipt.Result == "SUCCESS"
		} else {
			success = info.BlockNumber > 0
		}

		writeJSON(w, http.StatusOK, transactionInfoResponse{
			Found:   info.Found,
			Success: success,
		})
	}
}
