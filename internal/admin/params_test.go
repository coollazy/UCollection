package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestUpdateParamsHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{
		"validity_seconds":                   {"1800"},
		"amount_tolerance_percent":           {"2.5"},
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
	if validity != 1800 || tolerance != 2.5 || stallTimeout != 600 {
		t.Errorf("got validity=%d tolerance=%v stallTimeout=%d, want 1800/2.5/600", validity, tolerance, stallTimeout)
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
		"amount_tolerance_percent":           {"1"},
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

	form := url.Values{"validity_seconds": {"-5"}, "amount_tolerance_percent": {"1"}}
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
