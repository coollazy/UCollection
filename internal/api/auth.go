package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// timestampWindow is the max allowed clock skew between X-Timestamp and
// the server's own time (技術架構設計第7節「認證：API Key + HMAC簽章」防重放).
const timestampWindow = 5 * time.Minute

// authenticate verifies X-API-Key/X-Timestamp/X-Signature against
// api_keys. It deliberately returns the same errInvalidSignature for every
// failure mode (unknown key, revoked key, bad signature, expired
// timestamp) — distinguishing them in the response would let an attacker
// probe which API keys exist.
//
// The signed string is timestamp+method+RequestURI+body. RequestURI
// (path+query, exactly as sent) is used rather than just the path: for
// GET /api/v1/orders?merchant_order_no=X, the query string is the only
// varying input, so leaving it out of the signature would let a valid
// signature for one merchant_order_no be replayed against another (第7節
// only says "path", this closes that gap — see this module's plan notes).
func authenticate(ctx context.Context, pool *store.Pool, r *http.Request, body []byte) *apiError {
	apiKey := r.Header.Get("X-API-Key")
	timestampHeader := r.Header.Get("X-Timestamp")
	signature := r.Header.Get("X-Signature")
	if apiKey == "" || timestampHeader == "" || signature == "" {
		return errInvalidSignature
	}

	timestamp, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return errInvalidSignature
	}
	skew := time.Since(time.Unix(timestamp, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > timestampWindow {
		return errInvalidSignature
	}

	keyHash := sha256.Sum256([]byte(apiKey))
	var secret string
	err = pool.QueryRow(ctx, `
		SELECT secret FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL
	`, hex.EncodeToString(keyHash[:])).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInvalidSignature
	}
	if err != nil {
		return newAPIError(http.StatusInternalServerError, "INTERNAL_ERROR", "authentication failed")
	}

	signed := timestampHeader + r.Method + r.URL.RequestURI() + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return errInvalidSignature
	}
	return nil
}
