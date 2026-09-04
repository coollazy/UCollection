package auth

import (
	"context"
	"os"
	"testing"

	"github.com/coollazy/UCollection/internal/store"
)

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
	_, err := pool.Exec(context.Background(), `TRUNCATE admin_account, admin_sessions, audit_logs RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

const (
	testUsername = "admin"
	testPassword = "correct horse battery staple"
)

// bootstrapAccount creates the single admin_account row directly (bypassing
// EnsureAdminAccount, whose own behavior is tested separately) so other
// tests can start from a known account state.
func bootstrapAccount(t *testing.T, pool *store.Pool) Account {
	t.Helper()
	if err := EnsureAdminAccount(context.Background(), pool, testUsername, testPassword); err != nil {
		t.Fatalf("EnsureAdminAccount() error = %v", err)
	}
	account, err := loadAccount(context.Background(), pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	return account
}

func testDeps(pool *store.Pool) Deps {
	return Deps{Pool: pool, PublicOrigin: "https://admin.example", CookieSecure: false}
}
