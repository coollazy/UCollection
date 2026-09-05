package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestUpdateWebhookURLHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"webhook_url": {"https://merchant.example/hook"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config", strings.NewReader(form.Encode()))
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

	var got *string
	if err := pool.QueryRow(context.Background(), `SELECT webhook_url FROM system_params WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("query webhook_url: %v", err)
	}
	if got == nil || *got != "https://merchant.example/hook" {
		t.Errorf("webhook_url = %v, want https://merchant.example/hook", got)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'WEBHOOK_URL_CHANGED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("WEBHOOK_URL_CHANGED audit_logs count = %d, want 1", auditCount)
	}
}

func TestUpdateWebhookURLHandler_InvalidURL(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"webhook_url": {"not-a-url"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=invalid_url") {
		t.Errorf("Location = %q, want error=invalid_url", got)
	}

	var got *string
	if err := pool.QueryRow(context.Background(), `SELECT webhook_url FROM system_params WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("query webhook_url: %v", err)
	}
	if got != nil {
		t.Errorf("webhook_url = %v, want unchanged nil", got)
	}
}

func TestUpdateWebhookURLHandler_RequiresFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)
	if _, err := pool.Exec(context.Background(), `UPDATE admin_sessions SET last_totp_verified_at = now() - interval '1 hour'`); err != nil {
		t.Fatalf("stale totp: %v", err)
	}

	form := url.Values{"webhook_url": {"https://merchant.example/hook"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "/admin/reverify-totp") {
		t.Errorf("Location = %q, want redirect to /admin/reverify-totp", got)
	}
}

func TestUpdateWebhookSecretHandler_Generate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"mode": {"generate"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config/secret", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (generate mode shows secret once, no redirect)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	var dbSecret *string
	if err := pool.QueryRow(context.Background(), `SELECT webhook_secret FROM system_params WHERE id = 1`).Scan(&dbSecret); err != nil {
		t.Fatalf("query webhook_secret: %v", err)
	}
	if dbSecret == nil || *dbSecret == "" {
		t.Fatal("webhook_secret not set")
	}
	if !strings.Contains(string(body), *dbSecret) {
		t.Errorf("response body does not contain the generated secret")
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'WEBHOOK_SECRET_ROTATED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("WEBHOOK_SECRET_ROTATED audit_logs count = %d, want 1", auditCount)
	}
}

func TestUpdateWebhookSecretHandler_ManualRedirects(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"mode": {"manual"}, "secret": {"my-own-secret"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config/secret", strings.NewReader(form.Encode()))
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
		t.Fatalf("status = %d, want 303 (manual mode: operator already knows the value)", resp.StatusCode)
	}

	var dbSecret string
	if err := pool.QueryRow(context.Background(), `SELECT webhook_secret FROM system_params WHERE id = 1`).Scan(&dbSecret); err != nil {
		t.Fatalf("query webhook_secret: %v", err)
	}
	if dbSecret != "my-own-secret" {
		t.Errorf("webhook_secret = %q, want my-own-secret", dbSecret)
	}
}

func TestUpdateWebhookSecretHandler_ManualEmptyRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"mode": {"manual"}, "secret": {"  "}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config/secret", strings.NewReader(form.Encode()))
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
	if got := resp.Header.Get("Location"); !strings.Contains(got, "error=invalid_secret") {
		t.Errorf("Location = %q, want error=invalid_secret", got)
	}
}

func TestTestWebhookHandler_NotConfigured(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config/test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "尚未設定") {
		t.Errorf("body missing not-configured message, got: %s", body)
	}

	var deliveryCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM webhook_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count webhook_deliveries: %v", err)
	}
	if deliveryCount != 0 {
		t.Errorf("webhook_deliveries count = %d, want 0", deliveryCount)
	}
}

func TestTestWebhookHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer testSrv.Close()
	if _, err := pool.Exec(context.Background(), `UPDATE system_params SET webhook_url = $1, webhook_secret = 'test-secret' WHERE id = 1`, testSrv.URL); err != nil {
		t.Fatalf("set webhook config: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/webhook-config/test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "pong") {
		t.Errorf("body missing test response echo, got: %s", body)
	}
	if !strings.Contains(string(body), "200") {
		t.Errorf("body missing HTTP status 200, got: %s", body)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type LIKE 'WEBHOOK%'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("audit_logs count = %d, want 0 (test ping must not be audited)", auditCount)
	}
}
