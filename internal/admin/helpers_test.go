package admin

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/coollazy/UCollection/internal/auth"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/scanner"
	"github.com/coollazy/UCollection/internal/store"
)

// totpTestOpts mirrors internal/auth's own totpValidateOpts (period 30s,
// skew ±1, 6-digit SHA1) — duplicated here rather than exported from auth,
// same small-helper-per-package convention this project already uses (見
// internal/consolidation對CSP常數的處理).
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
// real account+secret to check the request's totp_code against, unlike the
// old freshness-only check which only read the session's timestamp.
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

const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

// sessionCookieName mirrors internal/auth's unexported cookie name — same
// black-box-table convention internal/consolidation's tests already use
// (see internal/consolidation/prepare_broadcast_test.go's own copy of this
// same helper).
const sessionCookieName = "ucollection_session"

func testPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping test that needs a real PostgreSQL instance")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetDB(t *testing.T, pool *store.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions,
			webhook_deliveries, webhook_delivery_attempts, consolidation_batches, consolidation_items,
			fee_topup_batches, fee_topup_items, admin_sessions, audit_logs, api_keys, admin_account
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE system_params SET validity_seconds = 900, amount_tolerance_percent = 1.5, confirmation_stall_timeout_seconds = 900,
			webhook_url = NULL, webhook_secret = NULL
		WHERE id = 1
	`); err != nil {
		t.Fatalf("seed system_params: %v", err)
	}
	// dashboardHandler reads scan_checkpoint via scanner.GetSyncStatus; in
	// production this row is guaranteed to exist before the HTTP server
	// accepts traffic (main.go calls scanner.EnsureCheckpoint before
	// ListenAndServe). resetDB doesn't truncate scan_checkpoint (it's not in
	// this package's TRUNCATE list above), so mirror that same guarantee
	// here instead of relying on some other package's test having left a
	// row behind — idempotent (ON CONFLICT DO NOTHING), safe to call every
	// reset.
	if err := scanner.EnsureCheckpoint(context.Background(), pool); err != nil {
		t.Fatalf("ensure scan_checkpoint: %v", err)
	}
}

// newAPIKey inserts one api_keys row directly (bypassing the regenerate
// handler) so tests can set up an existing key to assert gets revoked.
// key_hash uses the exact hex(sha256(key)) scheme internal/api/auth.go
// validates against (see api_keys.go's regenerateAPIKey, which computes it
// the same way).
func newAPIKey(t *testing.T, pool *store.Pool, key, secret string) int64 {
	t.Helper()
	sum := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(sum[:])
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO api_keys (key_hash, secret) VALUES ($1, $2) RETURNING id
	`, keyHash, secret).Scan(&id)
	if err != nil {
		t.Fatalf("insert api_keys: %v", err)
	}
	return id
}

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

func newMasterWallet(t *testing.T, pool *store.Pool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO master_wallets (xpub, status) VALUES ($1, 'active') RETURNING id`, testXpub).Scan(&id)
	if err != nil {
		t.Fatalf("insert master_wallets: %v", err)
	}
	return id
}

func newOrder(t *testing.T, pool *store.Pool, walletID int64, merchantOrderNo string) order.Order {
	t.Helper()
	o, err := order.CreateOrder(context.Background(), pool, order.CreateParams{
		MerchantOrderNo:                 merchantOrderNo,
		MasterWalletID:                  walletID,
		TargetAmount:                    100_000000,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          1.5,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	return o
}

func newTestServer(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	authDeps := auth.Deps{Pool: deps.Pool, PublicOrigin: "https://admin.example", CookieSecure: false}
	RegisterRoutes(mux, deps, authDeps)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// noRedirectClient never follows redirects, so tests can assert on the
// redirect itself (status + Location) instead of whatever page it points
// to (which may not even be registered on this package's test mux).
var noRedirectClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}
