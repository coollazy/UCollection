package consolidation

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/coollazy/UCollection/internal/auth"
	"github.com/coollazy/UCollection/internal/store"
)

// totpTestOpts mirrors internal/auth's own totpValidateOpts (period 30s,
// skew ±1, 6-digit SHA1) — duplicated here rather than exported from auth,
// same small-helper-per-package convention this project already uses.
var totpTestOpts = totp.ValidateOpts{
	Period:    30,
	Skew:      1,
	Digits:    otp.DigitsSix,
	Algorithm: otp.AlgorithmSHA1,
}

func totpCodeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, at, totpTestOpts)
	if err != nil {
		t.Fatalf("GenerateCodeCustom() error = %v", err)
	}
	return code
}

// newActiveSessionWithTOTP is newActiveSessionCookie plus an admin_account
// row carrying a known TOTP secret — RequireTOTPCode (ADR-0016: every
// high-risk POST/DELETE verifies a code fresh in the same request) needs a
// real account+secret to check the request's totp_code against.
func newActiveSessionWithTOTP(t *testing.T, pool *store.Pool) (cookie *http.Cookie, secret string) {
	t.Helper()
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "UCollection", AccountName: "admin"})
	if err != nil {
		t.Fatalf("totp.Generate() error = %v", err)
	}
	secret = key.Secret()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO admin_account (username, password_hash, totp_secret) VALUES ('admin', 'unused', $1)
	`, secret); err != nil {
		t.Fatalf("insert admin_account: %v", err)
	}
	return newActiveSessionCookie(t, pool), secret
}

// sessionCookieName mirrors internal/auth's unexported cookie name
// ("ucollection_session") — this package can't import that constant (it's
// intentionally unexported), so this integration test treats
// admin_sessions' token_hash contract (SHA-256 hex of the raw cookie
// token, documented in 技術架構設計第9節) as a stable black-box interface,
// the same way other packages' tests insert directly into tables they
// don't own rather than reaching into another package's unexported
// internals.
const sessionCookieName = "ucollection_session"

func newActiveSessionCookie(t *testing.T, pool *store.Pool) *http.Cookie {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	_, err := pool.Exec(context.Background(), `
		INSERT INTO admin_sessions (token_hash, status, expires_at, last_seen_at, last_totp_verified_at)
		VALUES ($1, 'active', now() + interval '1 hour', now(), now())
	`, tokenHash)
	if err != nil {
		t.Fatalf("insert admin_sessions: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token} //nolint:gosec // test-only outgoing request cookie, not a real Set-Cookie response
}

func newTestMux(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	authDeps := auth.Deps{Pool: deps.Pool, PublicOrigin: "https://admin.example", CookieSecure: false}
	RegisterRoutes(mux, deps, authDeps)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// noRedirectClient never follows redirects — RequireSession issues a 303 to
// /admin/login for unauthenticated requests, and /admin/login isn't
// registered on this package's test mux (only consolidation's own routes
// are), so following it would just 404 and mask the 303 we actually want
// to assert on.
var noRedirectClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// postJSON sends the totp_code the same way page.js's fetch()-based
// prepare/broadcast calls will (X-Totp-Code header, since these requests
// have a JSON body, not a form — ADR-0016's totpCodeFromRequest checks the
// header first for exactly this reason). code == "" omits the header
// entirely (used by tests specifically checking the missing-code case).
func postJSON(t *testing.T, srv *httptest.Server, cookie *http.Cookie, code, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if code != "" {
		req.Header.Set("X-Totp-Code", code)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// totpCodeSequence returns a function producing a fresh, non-colliding TOTP
// code on each call (step 0 = now, step 1 = +30s) — tests that prepare then
// broadcast need a distinct valid code per request, since RequireTOTPCode
// consumes each step via last_totp_step and rejects reusing one (見
// internal/auth的TOTP重放防護). Only steps 0 and 1 are usable within a
// single fast-running test: verifyTOTPCode's ±1 period skew window is
// centered on whatever "now" is at the moment of validation (still
// essentially the same instant this function was first called), so a
// step-2-or-later code falls outside that window and simply never
// validates — it's not a replay, it never matches at all. A third real
// step-up verification needs a genuine ~30s of elapsed wall-clock time, so
// tests needing more than two just call the target handler directly
// instead (見callSignPageHandler、prepare_broadcast_flow_test.go's second
// broadcast-attempt assertions for this pattern).
func totpCodeSequence(t *testing.T, secret string) func() string {
	t.Helper()
	step := 0
	return func() string {
		code := totpCodeAt(t, secret, time.Now().Add(time.Duration(step)*30*time.Second))
		step++
		return code
	}
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}
