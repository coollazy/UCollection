package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestUpdateParamsHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	var toleranceBefore float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&toleranceBefore); err != nil {
		t.Fatalf("query seeded tolerance: %v", err)
	}

	form := url.Values{
		"validity_seconds":                   {"1800"},
		"confirmation_stall_timeout_seconds": {"600"},
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params", strings.NewReader(form.Encode()))
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

	var validity, stallTimeout int64
	var tolerance float64
	if err := pool.QueryRow(context.Background(), `
		SELECT validity_seconds, amount_tolerance_percent, confirmation_stall_timeout_seconds FROM system_params WHERE id = 1
	`).Scan(&validity, &tolerance, &stallTimeout); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if validity != 1800 || stallTimeout != 600 {
		t.Errorf("got validity=%d stallTimeout=%d, want 1800/600", validity, stallTimeout)
	}
	if tolerance != toleranceBefore {
		t.Errorf("amount_tolerance_percent = %v, want unchanged %v (this route no longer touches it)", tolerance, toleranceBefore)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'PARAMS_CHANGED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("PARAMS_CHANGED audit_logs count = %d, want 1", auditCount)
	}
}

func TestUpdateParamsHandler_BlankStallTimeoutFallsBackToValidity(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{
		"validity_seconds":                   {"1200"},
		"confirmation_stall_timeout_seconds": {""},
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params", strings.NewReader(form.Encode()))
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

	var stallTimeout *int64
	if err := pool.QueryRow(context.Background(), `SELECT confirmation_stall_timeout_seconds FROM system_params WHERE id = 1`).Scan(&stallTimeout); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if stallTimeout == nil || *stallTimeout != 1200 {
		t.Errorf("confirmation_stall_timeout_seconds = %v, want 1200 (fell back to validity_seconds, not NULL)", stallTimeout)
	}
}

func TestUpdateParamsHandler_InvalidValiditySeconds(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"validity_seconds": {"-5"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=invalid_validity_seconds") {
		t.Errorf("Location = %q, want error=invalid_validity_seconds", got)
	}

	var validity int64
	if err := pool.QueryRow(context.Background(), `SELECT validity_seconds FROM system_params WHERE id = 1`).Scan(&validity); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if validity != 900 { // seeded by resetDB, must remain unchanged
		t.Errorf("validity_seconds = %d, want unchanged 900", validity)
	}
}

func TestUpdateToleranceHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	form := url.Values{"amount_tolerance_percent": {"2.5"}, "totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params/tolerance", strings.NewReader(form.Encode()))
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

	var tolerance float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&tolerance); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if tolerance != 2.5 {
		t.Errorf("amount_tolerance_percent = %v, want 2.5", tolerance)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'AMOUNT_TOLERANCE_CHANGED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("AMOUNT_TOLERANCE_CHANGED audit_logs count = %d, want 1", auditCount)
	}
}

func TestUpdateToleranceHandler_InvalidPercent(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	var toleranceBefore float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&toleranceBefore); err != nil {
		t.Fatalf("query seeded tolerance: %v", err)
	}

	form := url.Values{"amount_tolerance_percent": {"-1"}, "totp_code": {totpCodeAt(t, secret, time.Now())}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params/tolerance", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=invalid_tolerance_percent") {
		t.Errorf("Location = %q, want error=invalid_tolerance_percent", got)
	}

	var tolerance float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&tolerance); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if tolerance != toleranceBefore {
		t.Errorf("amount_tolerance_percent = %v, want unchanged %v", tolerance, toleranceBefore)
	}
}

// TestUpdateToleranceHandler_RequireFreshTOTP 階段07全系統審查發現：
// amount_tolerance_percent一旦被調高，會讓之後新訂單的短付更容易被系統自動判定
// 為COMPLETED，攻擊者僅取得session cookie（未取得2FA裝置）即可誤導商戶財務判斷
// ——因此這條路由疊加RequireTOTPCode；ADR-0016改為每次送出都要求驗證碼（不再是
// 「距上次驗證是否超過15分鐘」），這裡驗證缺漏totp_code時會被擋下、導回原頁面
// （不是導向/admin/reverify-totp，那是給GET頁面用的）。
func TestUpdateToleranceHandler_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	var toleranceBefore float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&toleranceBefore); err != nil {
		t.Fatalf("query seeded tolerance: %v", err)
	}

	form := url.Values{"amount_tolerance_percent": {"99"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/params/tolerance", strings.NewReader(form.Encode()))
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
		t.Fatalf("status = %d, want 303 (missing totp_code)", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Path != "/admin/params" || loc.Query().Get("totp_error") != "missing" {
		t.Errorf("Location = %q, want /admin/params?totp_error=missing", resp.Header.Get("Location"))
	}

	var tolerance float64
	if err := pool.QueryRow(context.Background(), `SELECT amount_tolerance_percent FROM system_params WHERE id = 1`).Scan(&tolerance); err != nil {
		t.Fatalf("query system_params: %v", err)
	}
	if tolerance != toleranceBefore {
		t.Errorf("amount_tolerance_percent = %v, want unchanged %v (must not have been updated)", tolerance, toleranceBefore)
	}
}
