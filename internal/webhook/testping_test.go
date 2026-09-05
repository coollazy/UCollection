package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendTestPing_NotConfigured(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	_, _, err := SendTestPing(context.Background(), pool, nil)
	if !errors.Is(err, ErrWebhookNotConfigured) {
		t.Fatalf("SendTestPing() error = %v, want ErrWebhookNotConfigured", err)
	}
}

func TestSendTestPing_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	setWebhookConfig(t, pool, srv.URL, "test-secret")

	status, _, err := SendTestPing(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("SendTestPing() error = %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}

	var payload testPingPayload
	if err := json.Unmarshal([]byte(receivedBody), &payload); err != nil {
		t.Fatalf("unmarshal received body: %v", err)
	}
	if payload.EventType != "TEST_PING" {
		t.Errorf("event_type = %q, want TEST_PING", payload.EventType)
	}
	if payload.EventID == "" {
		t.Error("event_id is empty")
	}

	var deliveryCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count webhook_deliveries: %v", err)
	}
	if deliveryCount != 0 {
		t.Errorf("webhook_deliveries count = %d, want 0 (test ping must not be persisted)", deliveryCount)
	}
}

func TestSendTestPing_NonSuccessStatusReturnedNotError(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	setWebhookConfig(t, pool, srv.URL, "test-secret")

	status, _, err := SendTestPing(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("SendTestPing() error = %v, want nil (non-2xx is a valid test result, not a Go error)", err)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
}
