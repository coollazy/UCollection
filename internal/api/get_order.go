package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

type transitionResponse struct {
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	ChangedBy  string `json:"changed_by"`
	Note       string `json:"note,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// orderDetailResponse matches 技術架構設計第7節「查詢API」：狀態、累計已偵測金額
// （only_confirmed=false）、累計已確認金額（confirmed=true）、完整狀態變更歷史.
type orderDetailResponse struct {
	ID              int64                `json:"id"`
	MerchantOrderNo string               `json:"merchant_order_no"`
	Address         string               `json:"address"`
	Status          string               `json:"status"`
	TargetAmount    string               `json:"target_amount"`
	DetectedAmount  string               `json:"detected_amount"`
	ConfirmedAmount string               `json:"confirmed_amount"`
	ExpiresAt       string               `json:"expires_at"`
	Transitions     []transitionResponse `json:"transitions"`
}

func getOrderByIDHandler(pool *store.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, errOrderNotFound)
			return
		}
		writeOrderDetail(w, r, pool, func(ctx context.Context) (order.Order, error) {
			return order.GetByID(ctx, pool, id)
		})
	}
}

func getOrderByMerchantOrderNoHandler(pool *store.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		no := r.URL.Query().Get("merchant_order_no")
		if no == "" {
			writeError(w, errInvalidRequest)
			return
		}
		writeOrderDetail(w, r, pool, func(ctx context.Context) (order.Order, error) {
			return order.GetByMerchantOrderNo(ctx, pool, no)
		})
	}
}

func writeOrderDetail(w http.ResponseWriter, r *http.Request, pool *store.Pool, fetch func(context.Context) (order.Order, error)) {
	ctx := r.Context()
	o, err := fetch(ctx)
	if errors.Is(err, order.ErrOrderNotFound) {
		writeError(w, errOrderNotFound)
		return
	}
	if err != nil {
		writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed"))
		return
	}

	detected, err := order.DetectedAmount(ctx, pool, o.ID)
	if err != nil {
		writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "aggregate failed"))
		return
	}
	confirmed, err := order.SumConfirmedAmount(ctx, pool, o.ID)
	if err != nil {
		writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "aggregate failed"))
		return
	}
	transitions, err := order.Transitions(ctx, pool, o.ID)
	if err != nil {
		writeError(w, newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "aggregate failed"))
		return
	}

	resp := orderDetailResponse{
		ID:              o.ID,
		MerchantOrderNo: o.MerchantOrderNo,
		Address:         o.Address,
		Status:          string(o.Status),
		TargetAmount:    strconv.FormatInt(o.TargetAmount, 10),
		DetectedAmount:  strconv.FormatInt(detected, 10),
		ConfirmedAmount: strconv.FormatInt(confirmed, 10),
		ExpiresAt:       o.ExpiresAt.UTC().Format(time.RFC3339),
	}
	for _, t := range transitions {
		resp.Transitions = append(resp.Transitions, transitionResponse{
			FromStatus: t.FromStatus,
			ToStatus:   t.ToStatus,
			ChangedBy:  t.ChangedBy,
			Note:       t.Note,
			CreatedAt:  t.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
