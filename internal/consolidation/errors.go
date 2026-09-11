package consolidation

import (
	"encoding/json"
	"net/http"
)

// apiError is this package's JSON error response shape, mirroring
// internal/api's convention: {"error_code": "...", "message": "..."}.
type apiError struct {
	Code    string
	Message string
	Status  int
}

func newAPIError(status int, code, message string) *apiError {
	return &apiError{Code: code, Message: message, Status: status}
}

var (
	errAPIInvalidRequest         = newAPIError(http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
	errAPIBatchNotFound          = newAPIError(http.StatusNotFound, "BATCH_NOT_FOUND", "batch not found")
	errAPIItemNotFound           = newAPIError(http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
	errAPIOrderNotFound          = newAPIError(http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
	errAPIBatchOrderMismatch     = newAPIError(http.StatusBadRequest, "BATCH_ORDER_MISMATCH", "order does not belong to this batch's master wallet")
	errAPIAlreadyConsolidated    = newAPIError(http.StatusConflict, "ALREADY_CONSOLIDATED", "order is already marked consolidated")
	errAPINoBalance              = newAPIError(http.StatusBadRequest, "NO_BALANCE", "address has no positive on-chain balance to consolidate")
	errAPIItemNotPending         = newAPIError(http.StatusConflict, "ITEM_NOT_PENDING", "item is not in pending status; broadcast can only be submitted once per prepared item")
	errAPITxIDMismatch           = newAPIError(http.StatusBadRequest, "TXID_MISMATCH", "submitted transaction's txID does not match the prepared item")
	errAPIOrderWalletMismatch    = newAPIError(http.StatusBadRequest, "ORDER_WALLET_MISMATCH", "order does not belong to the specified master wallet")
	errAPIEnergyEstimateReverted = newAPIError(http.StatusBadGateway, "ENERGY_ESTIMATE_REVERTED", "energy estimate simulation reverted on chain; fee estimate is not trustworthy")
	errAPIInternal               = newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
)

func writeError(w http.ResponseWriter, e *apiError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error_code": e.Code,
		"message":    e.Message,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
