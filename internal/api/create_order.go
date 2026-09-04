package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

type createOrderRequest struct {
	MerchantOrderNo string `json:"merchant_order_no"`
	TargetAmount    string `json:"target_amount"`
}

type orderResponse struct {
	ID              int64  `json:"id"`
	MerchantOrderNo string `json:"merchant_order_no"`
	Address         string `json:"address"`
	TargetAmount    string `json:"target_amount"`
	Status          string `json:"status"`
	ExpiresAt       string `json:"expires_at"`
	CheckoutURL     string `json:"checkout_url"`
}

func toOrderResponse(o order.Order, publicOrigin string) orderResponse {
	return orderResponse{
		ID:              o.ID,
		MerchantOrderNo: o.MerchantOrderNo,
		Address:         o.Address,
		TargetAmount:    strconv.FormatInt(o.TargetAmount, 10),
		Status:          string(o.Status),
		ExpiresAt:       o.ExpiresAt.UTC().Format(time.RFC3339),
		CheckoutURL:     fmt.Sprintf("%s/checkout/%s", publicOrigin, o.PublicToken),
	}
}

// createOrderHandler implements POST /api/v1/orders (技術架構設計第7節「建單
// API」), including the merchant_order_no idempotency rule: same
// merchant_order_no + same target_amount replays the original order (200);
// same merchant_order_no + different target_amount is a conflict (409).
func createOrderHandler(pool *store.Pool, publicOrigin string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createOrderRequest
		if err := json.Unmarshal(bodyFromContext(r), &req); err != nil || req.MerchantOrderNo == "" {
			writeError(w, errInvalidRequest)
			return
		}
		targetAmount, err := strconv.ParseInt(req.TargetAmount, 10, 64)
		if err != nil || targetAmount <= 0 {
			writeError(w, errInvalidRequest)
			return
		}

		ctx := r.Context()

		if existing, err := order.GetByMerchantOrderNo(ctx, pool, req.MerchantOrderNo); err == nil {
			respondIdempotent(w, existing, targetAmount, publicOrigin)
			return
		} else if !errors.Is(err, order.ErrOrderNotFound) {
			writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed"))
			return
		}

		params, apiErr := loadOrderParams(ctx, pool)
		if apiErr != nil {
			writeError(w, apiErr)
			return
		}
		masterWalletID, apiErr := activeMasterWalletID(ctx, pool)
		if apiErr != nil {
			writeError(w, apiErr)
			return
		}

		o, err := order.CreateOrder(ctx, pool, order.CreateParams{
			MerchantOrderNo:                 req.MerchantOrderNo,
			MasterWalletID:                  masterWalletID,
			TargetAmount:                    targetAmount,
			ValiditySeconds:                 params.validitySeconds,
			AmountTolerancePercent:          params.amountTolerancePercent,
			ConfirmationStallTimeoutSeconds: params.confirmationStallTimeoutSeconds,
		})
		if errors.Is(err, order.ErrDuplicateMerchantOrderNo) {
			// Lost a race with a concurrent identical request between our
			// existence check above and this insert — treat it the same
			// way as the idempotent path found above.
			existing, ferr := order.GetByMerchantOrderNo(ctx, pool, req.MerchantOrderNo)
			if ferr == nil {
				respondIdempotent(w, existing, targetAmount, publicOrigin)
				return
			}
		}
		if err != nil {
			writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "create order failed"))
			return
		}

		writeJSON(w, http.StatusCreated, toOrderResponse(o, publicOrigin))
	}
}

func respondIdempotent(w http.ResponseWriter, existing order.Order, targetAmount int64, publicOrigin string) {
	if existing.TargetAmount != targetAmount {
		writeError(w, errOrderConflict)
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(existing, publicOrigin))
}

type orderParams struct {
	validitySeconds                 int64
	amountTolerancePercent          float64
	confirmationStallTimeoutSeconds int64
}

// loadOrderParams reads the merchant's global defaults from system_params.
// validity_seconds/amount_tolerance_percent are required — the admin
// params page (not built yet) is where a merchant sets them, and there is
// no requirements-doc-specified fallback number to invent in their place
// (見本模組plan notes). confirmation_stall_timeout_seconds does have a
// documented fallback: 需求書5.2 says it defaults to validity_seconds when
// left blank.
func loadOrderParams(ctx context.Context, pool *store.Pool) (orderParams, *apiError) {
	var validitySeconds, stallTimeout *int64
	var tolerancePercent *float64
	err := pool.QueryRow(ctx, `
		SELECT validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds
		FROM system_params WHERE id = 1
	`).Scan(&validitySeconds, &tolerancePercent, &stallTimeout)
	if err != nil {
		return orderParams{}, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "load params failed")
	}
	if validitySeconds == nil || tolerancePercent == nil {
		return orderParams{}, errParamsNotConfigured
	}

	p := orderParams{validitySeconds: *validitySeconds, amountTolerancePercent: *tolerancePercent}
	if stallTimeout != nil {
		p.confirmationStallTimeoutSeconds = *stallTimeout
	} else {
		p.confirmationStallTimeoutSeconds = *validitySeconds
	}
	return p, nil
}

func activeMasterWalletID(ctx context.Context, pool *store.Pool) (int64, *apiError) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM master_wallets WHERE status = 'active' ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errNoActiveMasterWallet
	}
	if err != nil {
		return 0, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "load master wallet failed")
	}
	return id, nil
}
