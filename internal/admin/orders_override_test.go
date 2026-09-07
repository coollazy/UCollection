package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestOverrideStatusHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "override-1")
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status = 'CONFIRMATION_STALLED' WHERE id = $1`, o.ID); err != nil {
		t.Fatalf("force STALLED: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"to_status": {"COMPLETED"}, "note": {"人工查證已收到款項"}, "totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/orders/%d/override-status", o.ID), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash=override_ok") {
		t.Errorf("Location = %q, want flash=override_ok", got)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Status != order.StatusCompleted {
		t.Errorf("status = %s, want COMPLETED", got.Status)
	}

	// order.ManualTransition already inserts the webhook_deliveries row
	// atomically — this just confirms the handler didn't somehow bypass it.
	var webhookCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_deliveries WHERE order_id = $1`, o.ID).Scan(&webhookCount); err != nil {
		t.Fatalf("count webhook_deliveries: %v", err)
	}
	if webhookCount != 1 {
		t.Errorf("webhook_deliveries count = %d, want 1", webhookCount)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'ORDER_MANUAL_OVERRIDE'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("ORDER_MANUAL_OVERRIDE audit_logs count = %d, want 1", auditCount)
	}
}

// TestOverrideStatusHandler_IllegalTransitionIsAudited covers 技術架構設計第5節
// 「非法轉換嘗試記錄到獨立稽核日誌」 for the case order.ManualTransition itself
// doesn't audit-log (see this Part's plan notes on why that's admin's job).
func TestOverrideStatusHandler_IllegalTransitionIsAudited(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "override-illegal-1")
	// stays PENDING — not in the manual whitelist's allowed-from set for
	// any target status.

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"to_status": {"COMPLETED"}, "note": {"嘗試繞過"}, "totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/orders/%d/override-status", o.ID), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash_error=transition_not_allowed") {
		t.Errorf("Location = %q, want flash_error=transition_not_allowed", got)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Status != order.StatusPending {
		t.Errorf("status = %s, want unchanged PENDING", got.Status)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'ILLEGAL_STATE_TRANSITION'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("ILLEGAL_STATE_TRANSITION audit_logs count = %d, want 1", auditCount)
	}
}

func TestOverrideStatusHandler_RequiresFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "override-stale-1")
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status = 'CONFIRMATION_STALLED' WHERE id = $1`, o.ID); err != nil {
		t.Fatalf("force STALLED: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)
	// No admin_account/totp_secret set up at all — ADR-0016: every POST
	// requires its own totp_code, checked fresh every time.

	form := url.Values{"to_status": {"COMPLETED"}, "note": {"test"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/orders/%d/override-status", o.ID), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Path != fmt.Sprintf("/admin/orders/%d", o.ID) || loc.Query().Get("totp_error") != "missing" {
		t.Errorf("Location = %q, want /admin/orders/%d?totp_error=missing (missing totp_code)", resp.Header.Get("Location"), o.ID)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Status != order.StatusConfirmationStalled {
		t.Errorf("status = %s, want unchanged CONFIRMATION_STALLED (step-up should have blocked the write)", got.Status)
	}
}
