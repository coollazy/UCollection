package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestResendNotificationHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "resend-1")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	if _, err := pool.Exec(context.Background(), `UPDATE system_params SET webhook_url = $1, webhook_secret = 'secret' WHERE id = 1`, target.URL); err != nil {
		t.Fatalf("set webhook config: %v", err)
	}

	var deliveryID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO webhook_deliveries (event_id, order_id, event_type, payload, status)
		VALUES ('evt-resend-1', $1, 'ORDER_COMPLETED', '{}', 'failed')
		RETURNING id
	`, ord.ID).Scan(&deliveryID)
	if err != nil {
		t.Fatalf("insert webhook_deliveries: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/notifications/%d/resend", deliveryID), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash=") {
		t.Errorf("Location = %q, want a flash= success redirect", got)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
		t.Fatalf("query delivery: %v", err)
	}
	if status != "delivered" {
		t.Errorf("status = %q, want delivered", status)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'NOTIFICATION_MANUAL_RESEND'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("NOTIFICATION_MANUAL_RESEND audit_logs count = %d, want 1", auditCount)
	}
}

func TestResendNotificationHandler_FailureStillAudits(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "resend-2")
	// webhook_url/secret left unset -> Resend fails to load config.

	var deliveryID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO webhook_deliveries (event_id, order_id, event_type, payload, status)
		VALUES ('evt-resend-2', $1, 'ORDER_COMPLETED', '{}', 'failed')
		RETURNING id
	`, ord.ID).Scan(&deliveryID)
	if err != nil {
		t.Fatalf("insert webhook_deliveries: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/notifications/%d/resend", deliveryID), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash_error=") {
		t.Errorf("Location = %q, want a flash_error= redirect", got)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
		t.Fatalf("query delivery: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want unchanged failed", status)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'NOTIFICATION_MANUAL_RESEND'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("NOTIFICATION_MANUAL_RESEND audit_logs count = %d, want 1 (must audit even on failure)", auditCount)
	}
}
