package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

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
	_, err := pool.Exec(ctx, `TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions, webhook_deliveries, webhook_delivery_attempts RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE system_params SET webhook_url = NULL, webhook_secret = NULL WHERE id = 1`); err != nil {
		t.Fatalf("reset system_params: %v", err)
	}
}

func setWebhookConfig(t *testing.T, pool *store.Pool, url, secret string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE system_params SET webhook_url = $1, webhook_secret = $2 WHERE id = 1`, url, secret)
	if err != nil {
		t.Fatalf("set webhook config: %v", err)
	}
}

func testOrderID(t *testing.T, pool *store.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	var walletID int64
	err := pool.QueryRow(ctx, `INSERT INTO master_wallets (xpub, status) VALUES ($1, 'active') RETURNING id`, testXpub).Scan(&walletID)
	if err != nil {
		t.Fatalf("insert master_wallets: %v", err)
	}
	o, err := order.CreateOrder(ctx, pool, order.CreateParams{
		MerchantOrderNo:                 randomHex(t),
		MasterWalletID:                  walletID,
		TargetAmount:                    100,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          0,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	return o.ID
}

func randomHex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(buf)
}

func insertDelivery(t *testing.T, pool *store.Pool, orderID int64, status string, nextRetryAt *time.Time) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO webhook_deliveries (event_id, order_id, event_type, payload, status, next_retry_at)
		VALUES ($1, $2, 'ORDER_COMPLETED', '{"test":true}', $3, $4)
		RETURNING id
	`, randomHex(t), orderID, status, nextRetryAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert webhook_deliveries: %v", err)
	}
	return id
}

func setUpdatedAt(t *testing.T, pool *store.Pool, deliveryID int64, at time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE webhook_deliveries SET updated_at = $1 WHERE id = $2`, at, deliveryID)
	if err != nil {
		t.Fatalf("set updated_at: %v", err)
	}
}

func loadDelivery(t *testing.T, pool *store.Pool, deliveryID int64) (status string, attemptCount int, nextRetryAt *time.Time) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT status, attempt_count, next_retry_at FROM webhook_deliveries WHERE id = $1`, deliveryID).
		Scan(&status, &attemptCount, &nextRetryAt)
	if err != nil {
		t.Fatalf("load delivery: %v", err)
	}
	return status, attemptCount, nextRetryAt
}

func insertOldAttempt(t *testing.T, pool *store.Pool, deliveryID int64, attemptedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO webhook_delivery_attempts (delivery_id, attempt_no, http_status, triggered_by, attempted_at)
		VALUES ($1, 1, 500, 'auto', $2)
	`, deliveryID, attemptedAt)
	if err != nil {
		t.Fatalf("insert old attempt: %v", err)
	}
}

func TestProcessOne_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)

	var receivedTS, receivedSig, receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTS = r.Header.Get("X-Webhook-Timestamp")
		receivedSig = r.Header.Get("X-Webhook-Signature")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	setWebhookConfig(t, pool, srv.URL, "test-secret")
	deliveryID := insertDelivery(t, pool, orderID, "pending", timePtr(time.Now()))

	deps := Deps{Pool: pool, HTTPClient: newHTTPClient()}
	if err := processOne(ctx, deps, deliveryID); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}

	status, attemptCount, _ := loadDelivery(t, pool, deliveryID)
	if status != "delivered" {
		t.Errorf("status = %q, want delivered", status)
	}
	if attemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", attemptCount)
	}

	wantSig := hmacHex(t, "test-secret", receivedTS+`{"test":true}`)
	if receivedSig != wantSig {
		t.Errorf("received signature = %q, want %q", receivedSig, wantSig)
	}
	if receivedBody != `{"test":true}` {
		t.Errorf("received body = %q", receivedBody)
	}
}

func TestProcessOne_FailureSchedulesRetry(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	setWebhookConfig(t, pool, srv.URL, "test-secret")
	deliveryID := insertDelivery(t, pool, orderID, "pending", timePtr(time.Now()))

	deps := Deps{Pool: pool, HTTPClient: newHTTPClient()}
	if err := processOne(ctx, deps, deliveryID); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}

	status, attemptCount, nextRetryAt := loadDelivery(t, pool, deliveryID)
	if status != "pending" {
		t.Errorf("status = %q, want pending (still within retry window)", status)
	}
	if attemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", attemptCount)
	}
	if nextRetryAt == nil || time.Until(*nextRetryAt) < 30*time.Second || time.Until(*nextRetryAt) > 90*time.Second {
		t.Errorf("next_retry_at = %v, want ~1 minute from now", nextRetryAt)
	}
}

func TestProcessOne_ExceedsRetryWindowMarksFailed(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	setWebhookConfig(t, pool, srv.URL, "test-secret")
	deliveryID := insertDelivery(t, pool, orderID, "pending", timePtr(time.Now()))
	insertOldAttempt(t, pool, deliveryID, time.Now().Add(-25*time.Hour))

	deps := Deps{Pool: pool, HTTPClient: newHTTPClient()}
	if err := processOne(ctx, deps, deliveryID); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}

	status, _, _ := loadDelivery(t, pool, deliveryID)
	if status != "failed" {
		t.Errorf("status = %q, want failed (retry window exceeded)", status)
	}
}

func TestPickBatch_ReclaimsStuckSending(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)

	deliveryID := insertDelivery(t, pool, orderID, "sending", nil)
	setUpdatedAt(t, pool, deliveryID, time.Now().Add(-3*time.Minute))

	ids, err := pickBatch(ctx, pool, 10)
	if err != nil {
		t.Fatalf("pickBatch() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != deliveryID {
		t.Fatalf("pickBatch() = %v, want [%d]", ids, deliveryID)
	}
}

func TestPickBatch_SkipLockedNoDoubleAssignment(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	orderID := testOrderID(t, pool)

	const n = 6
	for i := 0; i < n; i++ {
		insertDelivery(t, pool, orderID, "pending", timePtr(time.Now()))
	}

	var wg sync.WaitGroup
	results := make([][]int64, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids, err := pickBatch(context.Background(), pool, 3)
			if err != nil {
				t.Errorf("pickBatch() [%d] error = %v", i, err)
				return
			}
			results[i] = ids
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for _, ids := range results {
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("delivery %d picked by both concurrent calls", id)
			}
			seen[id] = true
		}
	}
}

func TestSweepAwaitingConfig(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)

	deliveryID := insertDelivery(t, pool, orderID, "awaiting_config", nil)

	if err := sweepAwaitingConfig(ctx, pool); err != nil {
		t.Fatalf("sweepAwaitingConfig() error = %v", err)
	}
	status, _, _ := loadDelivery(t, pool, deliveryID)
	if status != "awaiting_config" {
		t.Fatalf("status = %q, want still awaiting_config (config not set)", status)
	}

	setWebhookConfig(t, pool, "https://example.com/webhook", "secret")
	if err := sweepAwaitingConfig(ctx, pool); err != nil {
		t.Fatalf("sweepAwaitingConfig() error = %v", err)
	}
	status, _, nextRetryAt := loadDelivery(t, pool, deliveryID)
	if status != "pending" {
		t.Fatalf("status = %q, want pending after config set", status)
	}
	if nextRetryAt == nil {
		t.Fatal("next_retry_at is nil after promotion, want now()")
	}
}

func TestResend(t *testing.T) {
	pool := testPool(t)

	t.Run("success delivers", func(t *testing.T) {
		resetDB(t, pool)
		orderID := testOrderID(t, pool)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
		defer srv.Close()
		setWebhookConfig(t, pool, srv.URL, "secret")

		deliveryID := insertDelivery(t, pool, orderID, "failed", nil)
		if err := Resend(context.Background(), pool, nil, deliveryID); err != nil {
			t.Fatalf("Resend() error = %v", err)
		}
		status, _, _ := loadDelivery(t, pool, deliveryID)
		if status != "delivered" {
			t.Errorf("status = %q, want delivered", status)
		}
	})

	t.Run("failure leaves status unchanged and does not touch attempt_count", func(t *testing.T) {
		resetDB(t, pool)
		orderID := testOrderID(t, pool)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
		defer srv.Close()
		setWebhookConfig(t, pool, srv.URL, "secret")

		deliveryID := insertDelivery(t, pool, orderID, "failed", nil)
		if err := Resend(context.Background(), pool, nil, deliveryID); err != nil {
			t.Fatalf("Resend() error = %v", err)
		}
		status, attemptCount, nextRetryAt := loadDelivery(t, pool, deliveryID)
		if status != "failed" {
			t.Errorf("status = %q, want unchanged failed", status)
		}
		if attemptCount != 0 {
			t.Errorf("attempt_count = %d, want unchanged 0 (manual resend must not touch it)", attemptCount)
		}
		if nextRetryAt != nil {
			t.Errorf("next_retry_at = %v, want unchanged nil", nextRetryAt)
		}

		var manualAttempts int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_delivery_attempts WHERE delivery_id = $1 AND triggered_by = 'manual'`, deliveryID).Scan(&manualAttempts); err != nil {
			t.Fatalf("count manual attempts: %v", err)
		}
		if manualAttempts != 1 {
			t.Errorf("manual attempts = %d, want 1", manualAttempts)
		}
	})
}

func TestSendWebhook_RedirectTreatedAsFailure(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer target.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirecting.Close()

	result, err := sendWebhook(context.Background(), newHTTPClient(), redirecting.URL, "secret", []byte(`{}`))
	if err != nil {
		t.Fatalf("sendWebhook() error = %v", err)
	}
	if result.HTTPStatus < 300 || result.HTTPStatus >= 400 {
		t.Fatalf("HTTPStatus = %d, want a 3xx (not followed)", result.HTTPStatus)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func hmacHex(t *testing.T, secret, message string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
