package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestReactivateMasterWalletHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	activeID := newMasterWallet(t, pool) // active
	var inactiveID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO master_wallets (xpub, status, last_derived_index) VALUES ($1, 'inactive', 12) RETURNING id
	`, secondTestXpub).Scan(&inactiveID); err != nil {
		t.Fatalf("insert inactive wallet: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/master-wallets/%d/reactivate", inactiveID), strings.NewReader(form.Encode()))
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

	var activeStatus, reactivatedStatus string
	var reactivatedIndex int64
	if err := pool.QueryRow(context.Background(), `SELECT status FROM master_wallets WHERE id = $1`, activeID).Scan(&activeStatus); err != nil {
		t.Fatalf("query old active: %v", err)
	}
	if activeStatus != "inactive" {
		t.Errorf("previously-active wallet status = %q, want inactive", activeStatus)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status, last_derived_index FROM master_wallets WHERE id = $1`, inactiveID).Scan(&reactivatedStatus, &reactivatedIndex); err != nil {
		t.Fatalf("query reactivated: %v", err)
	}
	if reactivatedStatus != "active" {
		t.Errorf("reactivated wallet status = %q, want active", reactivatedStatus)
	}
	if reactivatedIndex != 12 {
		t.Errorf("reactivated wallet last_derived_index = %d, want unchanged 12", reactivatedIndex)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'MASTER_WALLET_REACTIVATED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("MASTER_WALLET_REACTIVATED audit_logs count = %d, want 1", auditCount)
	}
}

func TestReactivateMasterWalletHandler_AlreadyActive(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	activeID := newMasterWallet(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/master-wallets/%d/reactivate", activeID), strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=already_active") {
		t.Errorf("Location = %q, want error=already_active", got)
	}

	// RequireTOTPCode itself logs TOTP_STEPUP_SUCCESS on every valid code
	// (ADR-0016) — that's expected and unrelated to this assertion, which
	// is specifically that the no-op reactivate attempt itself didn't log
	// anything beyond the step-up verification.
	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type != 'TOTP_STEPUP_SUCCESS'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("audit_logs count = %d, want 0 (no-op must not log, aside from the step-up verification itself)", auditCount)
	}
}

func TestReactivateMasterWalletHandler_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets/99999/reactivate", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=not_found") {
		t.Errorf("Location = %q, want error=not_found", got)
	}
}
