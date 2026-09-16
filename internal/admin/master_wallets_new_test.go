package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/tronclient"
)

const secondTestXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaSecondXpb"

// TestMasterWalletNewPageHandler_SetsStrictCSP exercises masterWalletNewPageHandler
// directly rather than through the full mux: ADR-0016 makes RequireTOTPCode
// unconditionally redirect every GET to /admin/reverify-totp first (no
// freshness grace window), so a request routed through the real mux never
// reaches this handler without a full login+TOTP-code round trip. CSP-header
// setting is this handler's own responsibility regardless of what gates it,
// so testing it in isolation is both simpler and still exercises the real
// behavior under test.
func TestMasterWalletNewPageHandler_SetsStrictCSP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/master-wallets/new", nil)
	masterWalletNewPageHandler(deps).ServeHTTP(rec, req)
	resp := rec.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })

	got := resp.Header.Get("Content-Security-Policy")
	if got != masterWalletNewCSPHeader {
		t.Errorf("Content-Security-Policy = %q, want %q", got, masterWalletNewCSPHeader)
	}
	if strings.Contains(got, "unsafe-inline") && !strings.Contains(got, "style-src") {
		t.Errorf("CSP allows unsafe-inline outside style-src: %q", got)
	}
	// 階段07全系統審查發現：此常數的doc comment宣稱「逐字複製」
	// internal/consolidation.signCSPHeader，但曾經漏掉frame-ancestors
	// 'none'（clickjacking防護）——這裡是輸入助記詞的頁面，跟簽名頁同等級
	// 風險，顯式斷言這段存在，避免之後又悄悄漏掉卻只靠字串完全比對測不出來
	// （完全比對只要兩邊「一起」漏掉同一段就測不出差異）。
	if !strings.Contains(got, "frame-ancestors 'none'") {
		t.Errorf("CSP missing frame-ancestors 'none': %q", got)
	}
}

// TestCreateMasterWalletHandler_ExistingActiveWallet_NewOneCreatedInactive
// 新增代收主錢包不再自動把既有使用中的那筆切成停用（使用者拍板：只有「目前
// 沒有任何使用中錢包」時新紀錄才自動active，避免系統沒有導引Gate可用；已有
// 使用中錢包時，新紀錄以inactive新增，既有那筆維持不動，商戶要切換過去需
// 另外按「恢復使用中」）。
func TestCreateMasterWalletHandler_ExistingActiveWallet_NewOneCreatedInactive(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	existingID := newMasterWallet(t, pool) // active, testXpub

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: secondTestXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Totp-Code", totpCodeAt(t, secret, time.Now()))
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
	if oldStatus != "active" {
		t.Errorf("old wallet status = %q, want unchanged active", oldStatus)
	}
	var newLastDerivedIndex int64
	if err := pool.QueryRow(context.Background(), `SELECT status, last_derived_index FROM master_wallets WHERE xpub = $1`, secondTestXpub).Scan(&newStatus, &newLastDerivedIndex); err != nil {
		t.Fatalf("query new wallet: %v", err)
	}
	if newStatus != "inactive" {
		t.Errorf("new wallet status = %q, want inactive", newStatus)
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

// TestCreateMasterWalletHandler_NoExistingActiveWallet_NewOneAutoActivates
// 涵蓋首次設定（或既有全部已停用）的情境：系統必須隨時有一個使用中的代收
// 主錢包可當導引Gate（internal/order/masterwallet.go），否則沒辦法配發任何
// 訂單收款地址，所以這個分支新紀錄仍自動active，不需要商戶額外操作。
func TestCreateMasterWalletHandler_NoExistingActiveWallet_NewOneAutoActivates(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: secondTestXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Totp-Code", totpCodeAt(t, secret, time.Now()))
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

	var newStatus string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM master_wallets WHERE xpub = $1`, secondTestXpub).Scan(&newStatus); err != nil {
		t.Fatalf("query new wallet: %v", err)
	}
	if newStatus != "active" {
		t.Errorf("new wallet status = %q, want active (no pre-existing active wallet)", newStatus)
	}
}

func TestCreateMasterWalletHandler_Duplicate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	newMasterWallet(t, pool) // active, testXpub

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	body, _ := json.Marshal(createMasterWalletRequest{Xpub: testXpub})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/master-wallets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Totp-Code", totpCodeAt(t, secret, time.Now()))
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

// TestCreateMasterWalletHandler_RequiresFreshTOTP ADR-0016: every POST
// requires its own totp_code every time (no freshness grace window) — a
// request with no code redirects to returnTo ("/admin/master-wallets")
// with totp_error=missing, not to the separate /admin/reverify-totp page
// (that's only for GET-registered routes, which have no body to carry a
// code in).
func TestCreateMasterWalletHandler_RequiresFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

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
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (missing totp_code)", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Path != "/admin/master-wallets" || loc.Query().Get("totp_error") != "missing" {
		t.Fatalf("Location = %q, want /admin/master-wallets?totp_error=missing", resp.Header.Get("Location"))
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM master_wallets`).Scan(&count); err != nil {
		t.Fatalf("count master_wallets: %v", err)
	}
	if count != 0 {
		t.Errorf("master_wallets count = %d, want 0 (must not have been created)", count)
	}
}
