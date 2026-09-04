package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

const (
	testXpub         = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"
	testPublicOrigin = "https://pay.merchant.example"
)

func testPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping test that needs a real PostgreSQL instance")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetDB(t *testing.T, pool *store.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions, webhook_deliveries, api_keys RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE system_params SET webhook_url = NULL, webhook_secret = NULL,
			validity_seconds = NULL, amount_tolerance_percent = NULL, confirmation_stall_timeout_seconds = NULL
		WHERE id = 1
	`)
	if err != nil {
		t.Fatalf("reset system_params: %v", err)
	}
}

func setOrderParams(t *testing.T, pool *store.Pool, validitySeconds int64, tolerancePercent float64, stallTimeoutSeconds *int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE system_params SET validity_seconds = $1, amount_tolerance_percent = $2, confirmation_stall_timeout_seconds = $3
		WHERE id = 1
	`, validitySeconds, tolerancePercent, stallTimeoutSeconds)
	if err != nil {
		t.Fatalf("set order params: %v", err)
	}
}

func newActiveMasterWallet(t *testing.T, pool *store.Pool, xpub string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO master_wallets (xpub, status) VALUES ($1, 'active') RETURNING id`, xpub).Scan(&id)
	if err != nil {
		t.Fatalf("insert master_wallets: %v", err)
	}
	return id
}

// newAPIKey inserts a usable api_keys row and returns the raw key value
// (what goes in X-API-Key) and the plaintext HMAC secret — mirroring how
// admin's (not yet built) key-issuance flow would hand these to a merchant
// once, before only the hash is retained server-side (CLAUDE.md 安全鐵律8).
func newAPIKey(t *testing.T, pool *store.Pool) (rawKey, secret string) {
	t.Helper()
	rawKey = randomHex(t)
	secret = randomHex(t)
	hash := sha256.Sum256([]byte(rawKey))
	_, err := pool.Exec(context.Background(), `INSERT INTO api_keys (key_hash, secret) VALUES ($1, $2)`, hex.EncodeToString(hash[:]), secret)
	if err != nil {
		t.Fatalf("insert api_keys: %v", err)
	}
	return rawKey, secret
}

func revokeAPIKey(t *testing.T, pool *store.Pool, rawKey string) {
	t.Helper()
	hash := sha256.Sum256([]byte(rawKey))
	_, err := pool.Exec(context.Background(), `UPDATE api_keys SET revoked_at = now() WHERE key_hash = $1`, hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatalf("revoke api_keys: %v", err)
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(buf)
}

func sign(secret, timestamp, method, requestURI, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + method + requestURI + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// signedRequest builds a request with valid X-API-Key/X-Timestamp/
// X-Signature headers for the given credentials.
func signedRequest(t *testing.T, method, target, body, apiKey, secret string) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sign(secret, ts, method, req.URL.RequestURI(), body))
	return req
}

func do(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode error response: %v (body=%s)", err, rec.Body.String())
	}
	return m
}

func TestAuthenticate_RejectsBadCredentials(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	mux := NewMux(pool, testPublicOrigin)

	t.Run("wrong signature", func(t *testing.T) {
		req := signedRequest(t, "GET", "/api/v1/orders?merchant_order_no=x", "", apiKey, secret)
		req.Header.Set("X-Signature", "0000")
		rec := do(mux, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if got := decodeError(t, rec)["error_code"]; got != "INVALID_SIGNATURE" {
			t.Errorf("error_code = %q, want INVALID_SIGNATURE", got)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		req := signedRequest(t, "GET", "/api/v1/orders?merchant_order_no=x", "", "not-a-real-key", secret)
		rec := do(mux, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("revoked key", func(t *testing.T) {
		revokeAPIKey(t, pool, apiKey)
		req := signedRequest(t, "GET", "/api/v1/orders?merchant_order_no=x", "", apiKey, secret)
		rec := do(mux, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("expired timestamp", func(t *testing.T) {
		newRaw, newSecret := newAPIKey(t, pool)
		ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
		req := httptest.NewRequest("GET", "/api/v1/orders?merchant_order_no=x", nil)
		req.Header.Set("X-API-Key", newRaw)
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Signature", sign(newSecret, ts, "GET", req.URL.RequestURI(), ""))
		rec := do(mux, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

func TestAuthenticate_SignatureCoversQueryString(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	mux := NewMux(pool, testPublicOrigin)

	// sign a request for merchant_order_no=A, then splice the same
	// signature onto a request for merchant_order_no=B — must be rejected,
	// proving the query string is actually covered by the signature.
	reqA := signedRequest(t, "GET", "/api/v1/orders?merchant_order_no=A", "", apiKey, secret)
	sig := reqA.Header.Get("X-Signature")
	ts := reqA.Header.Get("X-Timestamp")

	reqB := httptest.NewRequest("GET", "/api/v1/orders?merchant_order_no=B", nil)
	reqB.Header.Set("X-API-Key", apiKey)
	reqB.Header.Set("X-Timestamp", ts)
	reqB.Header.Set("X-Signature", sig)

	rec := do(mux, reqB)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (signature for a different query string must not validate)", rec.Code)
	}
}

func TestCreateOrder_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	newActiveMasterWallet(t, pool, testXpub)
	setOrderParams(t, pool, 900, 1.5, nil)
	mux := NewMux(pool, testPublicOrigin)

	body := `{"merchant_order_no":"order-1","target_amount":"100000000"}`
	req := signedRequest(t, "POST", "/api/v1/orders", body, apiKey, secret)
	rec := do(mux, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	var resp orderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.MerchantOrderNo != "order-1" {
		t.Errorf("MerchantOrderNo = %q, want order-1", resp.MerchantOrderNo)
	}
	if resp.Status != "PENDING" {
		t.Errorf("Status = %q, want PENDING", resp.Status)
	}
	wantCheckoutURL := fmt.Sprintf("%s/checkout/", testPublicOrigin)
	if !strings.HasPrefix(resp.CheckoutURL, wantCheckoutURL) {
		t.Errorf("CheckoutURL = %q, want prefix %q", resp.CheckoutURL, wantCheckoutURL)
	}
}

func TestCreateOrder_ParamsNotConfigured(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	newActiveMasterWallet(t, pool, testXpub)
	// setOrderParams intentionally not called — system_params stays NULL
	mux := NewMux(pool, testPublicOrigin)

	body := `{"merchant_order_no":"order-1","target_amount":"100000000"}`
	req := signedRequest(t, "POST", "/api/v1/orders", body, apiKey, secret)
	rec := do(mux, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeError(t, rec)["error_code"]; got != "PARAMS_NOT_CONFIGURED" {
		t.Errorf("error_code = %q, want PARAMS_NOT_CONFIGURED", got)
	}
}

func TestCreateOrder_NoActiveMasterWallet(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	setOrderParams(t, pool, 900, 1.5, nil)
	mux := NewMux(pool, testPublicOrigin)

	body := `{"merchant_order_no":"order-1","target_amount":"100000000"}`
	req := signedRequest(t, "POST", "/api/v1/orders", body, apiKey, secret)
	rec := do(mux, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := decodeError(t, rec)["error_code"]; got != "NO_ACTIVE_MASTER_WALLET" {
		t.Errorf("error_code = %q, want NO_ACTIVE_MASTER_WALLET", got)
	}
}

func TestCreateOrder_IdempotentReplaySameAmount(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	newActiveMasterWallet(t, pool, testXpub)
	setOrderParams(t, pool, 900, 1.5, nil)
	mux := NewMux(pool, testPublicOrigin)

	body := `{"merchant_order_no":"order-1","target_amount":"100000000"}`
	req1 := signedRequest(t, "POST", "/api/v1/orders", body, apiKey, secret)
	rec1 := do(mux, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want 201, body=%s", rec1.Code, rec1.Body.String())
	}
	var first orderResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &first)

	req2 := signedRequest(t, "POST", "/api/v1/orders", body, apiKey, secret)
	rec2 := do(mux, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request status = %d, want 200 (idempotent replay), body=%s", rec2.Code, rec2.Body.String())
	}
	var second orderResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if second.ID != first.ID {
		t.Errorf("replay returned a different order: id=%d, want %d", second.ID, first.ID)
	}
}

func TestCreateOrder_ConflictDifferentAmount(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	newActiveMasterWallet(t, pool, testXpub)
	setOrderParams(t, pool, 900, 1.5, nil)
	mux := NewMux(pool, testPublicOrigin)

	req1 := signedRequest(t, "POST", "/api/v1/orders", `{"merchant_order_no":"order-1","target_amount":"100000000"}`, apiKey, secret)
	if rec := do(mux, req1); rec.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}

	req2 := signedRequest(t, "POST", "/api/v1/orders", `{"merchant_order_no":"order-1","target_amount":"200000000"}`, apiKey, secret)
	rec2 := do(mux, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second request status = %d, want 409, body=%s", rec2.Code, rec2.Body.String())
	}
	if got := decodeError(t, rec2)["error_code"]; got != "ORDER_CONFLICT" {
		t.Errorf("error_code = %q, want ORDER_CONFLICT", got)
	}
}

func TestGetOrder_ByIDAndMerchantOrderNo(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	apiKey, secret := newAPIKey(t, pool)
	newActiveMasterWallet(t, pool, testXpub)
	setOrderParams(t, pool, 900, 1.5, nil)
	mux := NewMux(pool, testPublicOrigin)

	createReq := signedRequest(t, "POST", "/api/v1/orders", `{"merchant_order_no":"order-get","target_amount":"100000000"}`, apiKey, secret)
	createRec := do(mux, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}
	var created orderResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	t.Run("by id", func(t *testing.T) {
		req := signedRequest(t, "GET", fmt.Sprintf("/api/v1/orders/%d", created.ID), "", apiKey, secret)
		rec := do(mux, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var detail orderDetailResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if detail.MerchantOrderNo != "order-get" {
			t.Errorf("MerchantOrderNo = %q, want order-get", detail.MerchantOrderNo)
		}
		if detail.DetectedAmount != "0" || detail.ConfirmedAmount != "0" {
			t.Errorf("DetectedAmount/ConfirmedAmount = %s/%s, want 0/0", detail.DetectedAmount, detail.ConfirmedAmount)
		}
		if len(detail.Transitions) != 0 {
			t.Errorf("Transitions = %v, want empty (order never transitioned yet)", detail.Transitions)
		}
	})

	t.Run("by merchant_order_no", func(t *testing.T) {
		req := signedRequest(t, "GET", "/api/v1/orders?merchant_order_no=order-get", "", apiKey, secret)
		rec := do(mux, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var detail orderDetailResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &detail)
		if detail.ID != created.ID {
			t.Errorf("ID = %d, want %d", detail.ID, created.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := signedRequest(t, "GET", "/api/v1/orders/999999", "", apiKey, secret)
		rec := do(mux, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if got := decodeError(t, rec)["error_code"]; got != "ORDER_NOT_FOUND" {
			t.Errorf("error_code = %q, want ORDER_NOT_FOUND", got)
		}
	})
}
