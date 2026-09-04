// Package api implements the merchant-facing /api/v1 order create/query
// endpoints. See docs/開發流程框架-03-技術架構設計.md 第7節.
package api

import (
	"encoding/json"
	"net/http"
)

// apiError is the unified error response shape (技術架構設計第7節「錯誤回應
// 格式」): {"error_code": "...", "message": "..."}.
type apiError struct {
	Code    string
	Message string
	Status  int
}

func newAPIError(status int, code, message string) *apiError {
	return &apiError{Code: code, Message: message, Status: status}
}

var (
	errInvalidSignature     = newAPIError(http.StatusUnauthorized, "INVALID_SIGNATURE", "request signature invalid or expired")
	errOrderNotFound        = newAPIError(http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
	errOrderConflict        = newAPIError(http.StatusConflict, "ORDER_CONFLICT", "merchant_order_no already exists with a different target_amount")
	errParamsNotConfigured  = newAPIError(http.StatusInternalServerError, "PARAMS_NOT_CONFIGURED", "order parameters (validity/tolerance) have not been configured in the admin panel yet")
	errNoActiveMasterWallet = newAPIError(http.StatusServiceUnavailable, "NO_ACTIVE_MASTER_WALLET", "no active master wallet configured")
	errInvalidRequest       = newAPIError(http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
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
