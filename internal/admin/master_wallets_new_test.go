package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

const secondTestXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaSecondXpb"

func TestMasterWalletNewPageHandler_SetsStrictCSP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/master-wallets/new", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	got := resp.Header.Get("Content-Security-Policy")
	if got != masterWalletNewCSPHeader {
		t.Errorf("Content-Security-Policy = %q, want %q", got, masterWalletNewCSPHeader)
	}
	if strings.Contains(got, "unsafe-inline") && !strings.Contains(got, "style-src") {
		t.Errorf("CSP allows unsafe-inline outside style-src: %q", got)
	}
}

func TestCreateMasterWalletHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	existingID := newMasterWallet(t, pool) // active, testXpub

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: secondTestXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body: %s", resp.StatusCode, respBody)
	}
	var out createMasterWalletResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.OK || out.Redirect != "/admin/master-wallets" {
		t.Errorf("response = %+v, want ok redirect to /admin/master-wallets", out)
	}

	var oldStatus, newStatus string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM master_wallets WHERE id = $1`, existingID).Scan(&oldStatus); err != nil {
		t.Fatalf("query old wallet: %v", err)
	}
	if oldStatus != "inactive" {
		t.Errorf("old wallet status = %q, want inactive", oldStatus)
	}
	var newLastDerivedIndex int64
	if err := pool.QueryRow(context.Background(), `SELECT status, last_derived_index FROM master_wallets WHERE xpub = $1`, secondTestXpub).Scan(&newStatus, &newLastDerivedIndex); err != nil {
		t.Fatalf("query new wallet: %v", err)
	}
	if newStatus != "active" {
		t.Errorf("new wallet status = %q, want active", newStatus)
	}
	if newLastDerivedIndex != 0 {
		t.Errorf("new wallet last_derived_index = %d, want 0", newLastDerivedIndex)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'MASTER_WALLET_CREATED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("MASTER_WALLET_CREATED audit_logs count = %d, want 1", auditCount)
	}
}

func TestCreateMasterWalletHandler_Duplicate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	newMasterWallet(t, pool) // active, testXpub

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: testXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var out createMasterWalletResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.OK || out.Error != "duplicate" {
		t.Errorf("response = %+v, want error=duplicate", out)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM master_wallets`).Scan(&count); err != nil {
		t.Fatalf("count master_wallets: %v", err)
	}
	if count != 1 {
		t.Errorf("master_wallets count = %d, want unchanged 1", count)
	}
}

func TestCreateMasterWalletHandler_RequiresFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)
	if _, err := pool.Exec(context.Background(), `UPDATE admin_sessions SET last_totp_verified_at = now() - interval '1 hour'`); err != nil {
		t.Fatalf("stale totp: %v", err)
	}

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: secondTestXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (stale TOTP)", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "/admin/reverify-totp") {
		t.Errorf("Location = %q, want redirect to /admin/reverify-totp", got)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM master_wallets`).Scan(&count); err != nil {
		t.Fatalf("count master_wallets: %v", err)
	}
	if count != 0 {
		t.Errorf("master_wallets count = %d, want 0 (must not have been created)", count)
	}
}
