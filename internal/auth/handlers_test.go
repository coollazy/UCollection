package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

func newAuthTestServer(t *testing.T, deps Deps) (*httptest.Server, *http.Client) {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, deps)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return srv, client
}

func doPostForm(t *testing.T, client *http.Client, rawURL string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawURL, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func doGet(t *testing.T, client *http.Client, rawURL string) *http.Response {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func readPendingTOTPSecret(t *testing.T, pool *store.Pool) string {
	t.Helper()
	var secret *string
	err := pool.QueryRow(context.Background(), `SELECT pending_totp_secret FROM admin_sessions ORDER BY id DESC LIMIT 1`).Scan(&secret)
	if err != nil {
		t.Fatalf("read pending_totp_secret: %v", err)
	}
	if secret == nil {
		t.Fatal("pending_totp_secret is NULL")
	}
	return *secret
}

func countAuditLogs(t *testing.T, pool *store.Pool, actionType string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action_type = $1`, actionType).Scan(&count); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	return count
}

func TestLoginFlow_FirstTimeSetup(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	bootstrapAccount(t, pool)
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	resp := doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {testPassword}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login/totp-setup" {
		t.Fatalf("login: status=%d location=%q, want 303 to /admin/login/totp-setup", resp.StatusCode, resp.Header.Get("Location"))
	}

	getResp := doGet(t, client, srv.URL+"/admin/login/totp-setup")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET totp-setup: status = %d", getResp.StatusCode)
	}
	body := readBody(t, getResp)
	if !strings.Contains(body, "QR code") || !strings.Contains(body, "data:image/png;base64,") {
		t.Fatalf("totp-setup page missing expected QR markup: %s", body)
	}

	secret := readPendingTOTPSecret(t, pool)
	code := codeAt(t, secret, time.Now())

	submitResp := doPostForm(t, client, srv.URL+"/admin/login/totp-setup", url.Values{"code": {code}})
	if submitResp.StatusCode != http.StatusSeeOther || submitResp.Header.Get("Location") != "/admin/dashboard" {
		t.Fatalf("totp-setup submit: status=%d location=%q, want 303 to /admin/dashboard", submitResp.StatusCode, submitResp.Header.Get("Location"))
	}

	account, err := loadAccount(context.Background(), pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	if account.TOTPSecret == nil || *account.TOTPSecret != secret {
		t.Fatal("admin_account.totp_secret was not persisted from the setup flow")
	}

	if countAuditLogs(t, pool, "LOGIN_SUCCESS") != 1 {
		t.Fatal("expected exactly one LOGIN_SUCCESS audit log entry")
	}
	if countAuditLogs(t, pool, "TOTP_SETUP_SUCCESS") != 1 {
		t.Fatal("expected exactly one TOTP_SETUP_SUCCESS audit log entry")
	}

	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM admin_sessions ORDER BY id DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	if status != string(statusActive) {
		t.Fatalf("session status = %q, want active", status)
	}
}

func TestLoginFlow_ReturningUser(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	account := bootstrapAccount(t, pool)
	secret, err := newTOTPSecret(testUsername)
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE admin_account SET totp_secret = $1 WHERE id = $2`, secret, account.ID); err != nil {
		t.Fatalf("preset totp_secret: %v", err)
	}
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	resp := doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {testPassword}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login/totp" {
		t.Fatalf("login: status=%d location=%q, want 303 to /admin/login/totp", resp.StatusCode, resp.Header.Get("Location"))
	}

	code := codeAt(t, secret, time.Now())
	submitResp := doPostForm(t, client, srv.URL+"/admin/login/totp", url.Values{"code": {code}})
	if submitResp.StatusCode != http.StatusSeeOther || submitResp.Header.Get("Location") != "/admin/dashboard" {
		t.Fatalf("totp submit: status=%d location=%q, want 303 to /admin/dashboard", submitResp.StatusCode, submitResp.Header.Get("Location"))
	}

	if countAuditLogs(t, pool, "TOTP_VERIFY_SUCCESS") != 1 {
		t.Fatal("expected exactly one TOTP_VERIFY_SUCCESS audit log entry")
	}
}

func TestLoginFlow_WrongPassword(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	bootstrapAccount(t, pool)
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	resp := doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {"totally wrong password"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_sessions`).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("admin_sessions count = %d, want 0 after a failed login", count)
	}
	if countAuditLogs(t, pool, "LOGIN_FAILED") != 1 {
		t.Fatal("expected exactly one LOGIN_FAILED audit log entry")
	}
}

func TestTOTPReplayProtection(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	account := bootstrapAccount(t, pool)
	secret, err := newTOTPSecret(testUsername)
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE admin_account SET totp_secret = $1 WHERE id = $2`, secret, account.ID); err != nil {
		t.Fatalf("preset totp_secret: %v", err)
	}
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {testPassword}})
	code := codeAt(t, secret, time.Now())
	first := doPostForm(t, client, srv.URL+"/admin/login/totp", url.Values{"code": {code}})
	if first.StatusCode != http.StatusSeeOther {
		t.Fatalf("first totp submit: status = %d, want 303", first.StatusCode)
	}

	// Force the session stale so RequireFreshTOTP sends us to reverify, then
	// reuse the exact same code that just logged us in.
	if _, err := pool.Exec(context.Background(), `UPDATE admin_sessions SET last_totp_verified_at = now() - interval '16 minutes'`); err != nil {
		t.Fatalf("age last_totp_verified_at: %v", err)
	}
	reverifyGet := doGet(t, client, srv.URL+"/admin/password")
	if reverifyGet.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /admin/password: status = %d, want 303 to reverify", reverifyGet.StatusCode)
	}

	reverifyResp := doPostForm(t, client, srv.URL+"/admin/reverify-totp", url.Values{"code": {code}, "next": {"/admin/password"}})
	if reverifyResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reverify with replayed code: status = %d, want 401", reverifyResp.StatusCode)
	}
	body := readBody(t, reverifyResp)
	if !strings.Contains(body, "已被使用") {
		t.Fatalf("reverify response missing replay message: %s", body)
	}

	// Replayed codes must not count toward the 5-attempt lockout.
	var attemptCount int
	if err := pool.QueryRow(context.Background(), `SELECT totp_attempt_count FROM admin_sessions ORDER BY id DESC LIMIT 1`).Scan(&attemptCount); err != nil {
		t.Fatalf("query totp_attempt_count: %v", err)
	}
	if attemptCount != 0 {
		t.Fatalf("totp_attempt_count = %d, want 0 (replay must not count as a failure)", attemptCount)
	}
}

func TestSessionLockout_FiveConsecutiveFailures(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	account := bootstrapAccount(t, pool)
	secret, err := newTOTPSecret(testUsername)
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE admin_account SET totp_secret = $1 WHERE id = $2`, secret, account.ID); err != nil {
		t.Fatalf("preset totp_secret: %v", err)
	}
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {testPassword}})

	// The real code at "now" would accidentally succeed, so use a
	// certainly-wrong fixed string for all 5 attempts.
	wrongCode := "000000"
	if codeAt(t, secret, time.Now()) == wrongCode {
		t.Skip("random secret happened to make 000000 the real code")
	}

	var last *http.Response
	for i := 0; i < totpAttemptLimit; i++ {
		last = doPostForm(t, client, srv.URL+"/admin/login/totp", url.Values{"code": {wrongCode}})
	}

	if last.StatusCode != http.StatusSeeOther || last.Header.Get("Location") != "/admin/login?reason=locked" {
		t.Fatalf("5th failure: status=%d location=%q, want 303 to /admin/login?reason=locked", last.StatusCode, last.Header.Get("Location"))
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_sessions`).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("admin_sessions count = %d, want 0 after hitting the lockout threshold", count)
	}
	if countAuditLogs(t, pool, "SESSION_LOCKED") != 1 {
		t.Fatal("expected exactly one SESSION_LOCKED audit log entry")
	}
}

func TestPasswordChange_RequiresFreshTOTPAndClearsAllSessions(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	account := bootstrapAccount(t, pool)
	secret, err := newTOTPSecret(testUsername)
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE admin_account SET totp_secret = $1 WHERE id = $2`, secret, account.ID); err != nil {
		t.Fatalf("preset totp_secret: %v", err)
	}
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {testPassword}})
	code := codeAt(t, secret, time.Now())
	doPostForm(t, client, srv.URL+"/admin/login/totp", url.Values{"code": {code}})

	// ADR-0016: GET-protected pages have no body to carry a code in, so
	// every single visit bounces through /admin/reverify-totp first — no
	// grace window, not even right after a just-completed login.
	getResp := doGet(t, client, srv.URL+"/admin/password")
	if getResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /admin/password: status = %d, want 303 every time (no freshness grace window)", getResp.StatusCode)
	}
	loc, err := url.Parse(getResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Path != "/admin/reverify-totp" || loc.Query().Get("next") != "/admin/password" {
		t.Fatalf("Location = %q, want /admin/reverify-totp?next=/admin/password", getResp.Header.Get("Location"))
	}

	// POST /admin/password carries its own totp_code field — it does not
	// depend on GET /admin/password's separate reverify-totp challenge
	// checked above (ADR-0016: every high-risk POST verifies a code fresh
	// in the same request, independent of any other route's step-up
	// state). Only two real TOTP validations happen in this test (login's
	// and this one) so both stay comfortably inside the ±1 period (30s)
	// skew window without colliding with replay protection.
	const newPassword = "a-brand-new-12char-password" //nolint:gosec // test fixture value, not a real credential
	changeCode := codeAt(t, secret, time.Now().Add(30*time.Second)) // different step than the login code, avoid replay rejection
	changeResp := doPostForm(t, client, srv.URL+"/admin/password", url.Values{
		"current_password": {testPassword},
		"new_password":     {newPassword},
		"totp_code":        {changeCode},
	})
	if changeResp.StatusCode != http.StatusSeeOther || changeResp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("password change: status=%d location=%q, want 303 to /admin/login", changeResp.StatusCode, changeResp.Header.Get("Location"))
	}

	updated, err := loadAccount(context.Background(), pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	if !checkPassword(updated.PasswordHash, newPassword) {
		t.Fatal("password_hash was not updated to the new password")
	}
	if checkPassword(updated.PasswordHash, testPassword) {
		t.Fatal("old password still validates after change")
	}

	var sessionCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM admin_sessions`).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("admin_sessions count = %d, want 0 after password change", sessionCount)
	}
	if countAuditLogs(t, pool, "PASSWORD_CHANGED") != 1 {
		t.Fatal("expected exactly one PASSWORD_CHANGED audit log entry")
	}
}

func TestLogout(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	bootstrapAccount(t, pool)
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)
	token := activeSessionToken(t, deps)
	client.Jar.SetCookies(mustParseURL(t, srv.URL), []*http.Cookie{{Name: cookieName, Value: token}}) //nolint:gosec // test-only cookie injection, not a real response

	resp := doPostForm(t, client, srv.URL+"/admin/logout", url.Values{})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("logout: status=%d location=%q, want 303 to /admin/login", resp.StatusCode, resp.Header.Get("Location"))
	}

	if _, err := getSessionByToken(context.Background(), pool, token); err != errSessionNotFound {
		t.Fatalf("session row still exists after logout: err = %v", err)
	}
	if countAuditLogs(t, pool, "LOGOUT") != 1 {
		t.Fatal("expected exactly one LOGOUT audit log entry")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestIPRateLimit_LoginEndpoint(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	bootstrapAccount(t, pool)
	deps := testDeps(pool)
	srv, client := newAuthTestServer(t, deps)

	var last *http.Response
	for i := 0; i < ipRateLimitMax+1; i++ {
		last = doPostForm(t, client, srv.URL+"/admin/login", url.Values{"username": {testUsername}, "password": {"wrong password"}})
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status after %d failed attempts = %d, want 429", ipRateLimitMax+1, last.StatusCode)
	}
}
