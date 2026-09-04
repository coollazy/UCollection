package auth

import (
	"context"
	"testing"
)

func TestEnsureAdminAccount_CreatesOnEmptyTable(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := EnsureAdminAccount(ctx, pool, testUsername, testPassword); err != nil {
		t.Fatalf("EnsureAdminAccount() error = %v", err)
	}

	account, err := loadAccount(ctx, pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	if account.Username != testUsername {
		t.Fatalf("Username = %q, want %q", account.Username, testUsername)
	}
	if !checkPassword(account.PasswordHash, testPassword) {
		t.Fatal("bootstrapped password_hash doesn't match the bootstrap password")
	}
	if account.TOTPSecret != nil {
		t.Fatal("TOTPSecret should be nil until first login's setup flow completes")
	}
}

func TestEnsureAdminAccount_DoesNotOverwriteExisting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := EnsureAdminAccount(ctx, pool, testUsername, testPassword); err != nil {
		t.Fatalf("first EnsureAdminAccount() error = %v", err)
	}
	first, err := loadAccount(ctx, pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}

	// Simulate a restart where the operator changed the password via the
	// UI in between — the env vars (if still set) must not reset it.
	if err := EnsureAdminAccount(ctx, pool, "someone-else", "a-totally-different-password"); err != nil {
		t.Fatalf("second EnsureAdminAccount() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("admin_account row count = %d, want 1", count)
	}

	second, err := loadAccount(ctx, pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	if second.Username != first.Username || second.PasswordHash != first.PasswordHash {
		t.Fatal("second EnsureAdminAccount() overwrote the existing account")
	}
}

func TestEnsureAdminAccount_MissingCredentialsOnEmptyTable(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := EnsureAdminAccount(ctx, pool, "", ""); err == nil {
		t.Fatal("expected error when bootstrapping with no username/password set")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("admin_account row count = %d, want 0 after failed bootstrap", count)
	}
}

func TestEnsureAdminAccount_PasswordTooShort(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := EnsureAdminAccount(ctx, pool, testUsername, "short"); err == nil {
		t.Fatal("expected error for a password under 12 characters")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("admin_account row count = %d, want 0 after failed bootstrap", count)
	}
}
