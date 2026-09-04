package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// Account mirrors the single admin_account row.
type Account struct {
	ID                int64
	Username          string
	PasswordHash      string
	TOTPSecret        *string
	LastTOTPStep      *int64
	PasswordChangedAt time.Time
}

var errAccountNotFound = errors.New("auth: admin_account has no row (EnsureAdminAccount not run?)")

// loadAccount fetches the single admin_account row. There is always
// exactly one row once EnsureAdminAccount has run at startup (技術架構設計
// 第9節), so this doesn't filter by id.
func loadAccount(ctx context.Context, pool *store.Pool) (Account, error) {
	var a Account
	err := pool.QueryRow(ctx, `
		SELECT id, username, password_hash, totp_secret, last_totp_step, password_changed_at
		FROM admin_account
		LIMIT 1
	`).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.TOTPSecret, &a.LastTOTPStep, &a.PasswordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, errAccountNotFound
	}
	return a, err
}

// setTOTPSecretIfUnset implements the conditional write from 技術架構設計第9
// 節步驟2: only succeeds if totp_secret is still NULL, so a concurrent
// pending_2fa session can't clobber another session's already-completed
// setup. Returns false (no error) if another session won the race.
func setTOTPSecretIfUnset(ctx context.Context, pool *store.Pool, accountID int64, secret string) (bool, error) {
	tag, err := pool.Exec(ctx, `UPDATE admin_account SET totp_secret = $2 WHERE id = $1 AND totp_secret IS NULL`, accountID, secret)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func updateLastTOTPStep(ctx context.Context, pool *store.Pool, accountID, step int64) error {
	_, err := pool.Exec(ctx, `UPDATE admin_account SET last_totp_step = $2 WHERE id = $1`, accountID, step)
	return err
}

func updatePassword(ctx context.Context, pool *store.Pool, accountID int64, hash string) error {
	_, err := pool.Exec(ctx, `UPDATE admin_account SET password_hash = $2, password_changed_at = now() WHERE id = $1`, accountID, hash)
	return err
}
