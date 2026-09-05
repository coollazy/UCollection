package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

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
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/master-wallets/%d/reactivate", inactiveID), nil)
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
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/master-wallets/%d/reactivate", activeID), nil)
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=already_active") {
		t.Errorf("Location = %q, want error=already_active", got)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("audit_logs count = %d, want 0 (no-op must not log)", auditCount)
	}
}

func TestReactivateMasterWalletHandler_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets/99999/reactivate", nil)
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=not_found") {
		t.Errorf("Location = %q, want error=not_found", got)
	}
}
