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

	"github.com/coollazy/UCollection/internal/auth"
	"github.com/coollazy/UCollection/internal/store"
)

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

func postJSON(t *testing.T, srv *httptest.Server, cookie *http.Cookie, path string, body any) *http.Response {
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

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}
