package api

import (
	"context"
	"io"
	"net/http"

	"github.com/coollazy/UCollection/internal/store"
)

type bodyContextKey struct{}

// maxRequestBodyBytes bounds the body read before signature verification —
// defensive against a caller (even an authenticated one, since this read
// happens before auth) sending an absurdly large body.
const maxRequestBodyBytes = 1 << 20 // 1MB

// NewMux wires the three routes defined in 技術架構設計第7節, each behind
// authenticate (API Key + HMAC).
func NewMux(pool *store.Pool, publicOrigin string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/orders", withAuth(pool, createOrderHandler(pool, publicOrigin)))
	mux.Handle("GET /api/v1/orders/{id}", withAuth(pool, getOrderByIDHandler(pool)))
	mux.Handle("GET /api/v1/orders", withAuth(pool, getOrderByMerchantOrderNoHandler(pool)))
	return mux
}

func withAuth(pool *store.Pool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
		if err != nil {
			writeError(w, errInvalidRequest)
			return
		}
		if apiErr := authenticate(r.Context(), pool, r, body); apiErr != nil {
			writeError(w, apiErr)
			return
		}
		ctx := context.WithValue(r.Context(), bodyContextKey{}, body)
		next(w, r.WithContext(ctx))
	}
}

func bodyFromContext(r *http.Request) []byte {
	b, _ := r.Context().Value(bodyContextKey{}).([]byte)
	return b
}
