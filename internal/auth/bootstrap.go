package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coollazy/UCollection/internal/store"
)

// EnsureAdminAccount implements 技術架構設計第9節「帳號模型」: on startup, if
// admin_account has no row yet, create the single account from the given
// username/password (typically sourced from ADMIN_USERNAME/ADMIN_PASSWORD
// env vars). Once a row exists it is never overwritten, even if the env
// vars are still set on a later restart — that would silently reset a
// password the operator has since changed.
//
// This check happens at runtime against the DB, not in config.Load(): the
// env vars are only required the very first time the app boots against an
// empty database, so they can't be validated as unconditionally-required
// config.
func EnsureAdminAccount(ctx context.Context, pool *store.Pool, username, password string) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&count); err != nil {
		return fmt.Errorf("auth: count admin_account: %w", err)
	}
	if count > 0 {
		return nil
	}

	if username == "" || password == "" {
		return errors.New("auth: admin_account is empty and ADMIN_USERNAME/ADMIN_PASSWORD are not set")
	}
	if err := validatePasswordLength(password); err != nil {
		return fmt.Errorf("auth: bootstrap admin account: %w", err)
	}

	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hash bootstrap password: %w", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO admin_account (username, password_hash) VALUES ($1, $2)`, username, hash)
	if err != nil {
		return fmt.Errorf("auth: insert admin_account: %w", err)
	}
	return nil
}
