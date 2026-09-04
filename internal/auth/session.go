package auth

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// cookieName is the single session cookie used for both /admin/... and
// (later) /tron-proxy/... routes, hence Path=/ rather than /admin (技術架構
// 設計第9節「Session模型」).
const cookieName = "ucollection_session"

// Timeouts per 技術架構設計第9節「Session模型」/「高風險操作Step-up驗證」.
const (
	pendingSessionTTL = 5 * time.Minute
	activeSessionTTL  = 12 * time.Hour
	idleTimeout       = 30 * time.Minute
	freshTOTPWindow   = 15 * time.Minute
)

type sessionStatus string

const (
	statusPendingTOTP sessionStatus = "pending_2fa"
	statusActive      sessionStatus = "active"
)

// Session mirrors an admin_sessions row.
type Session struct {
	ID                 int64
	Status             sessionStatus
	PendingTOTPSecret  *string
	TOTPAttemptCount   int
	CreatedAt          time.Time
	ExpiresAt          time.Time
	LastSeenAt         time.Time
	LastTOTPVerifiedAt *time.Time
}

var errSessionNotFound = errors.New("auth: session not found")

func generateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

const sessionColumns = `id, status, pending_totp_secret, totp_attempt_count, created_at, expires_at, last_seen_at, last_totp_verified_at`

func scanSession(row pgx.Row) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.Status, &s.PendingTOTPSecret, &s.TOTPAttemptCount, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt, &s.LastTOTPVerifiedAt)
	return s, err
}

// createPendingSession starts the two-stage login (技術架構設計第9節步驟1):
// password already verified by the caller, this just opens a 5-minute
// pending_2fa session and returns the raw token for the cookie.
func createPendingSession(ctx context.Context, pool *store.Pool) (token string, sess Session, err error) {
	token, err = generateSessionToken()
	if err != nil {
		return "", Session{}, err
	}
	row := pool.QueryRow(ctx, `
		INSERT INTO admin_sessions (token_hash, status, expires_at)
		VALUES ($1, $2, now() + interval '5 minutes')
		RETURNING `+sessionColumns, hashToken(token), statusPendingTOTP)
	sess, err = scanSession(row)
	return token, sess, err
}

// getSessionByToken loads a session by its cookie token with no status or
// timeout enforcement — used by the login/totp-setup/totp/reverify
// handlers, which need to inspect a pending_2fa session directly (those
// routes are explicitly excluded from RequireSession's active-only gate,
// see 技術架構設計第9節「Session模型」).
func getSessionByToken(ctx context.Context, pool *store.Pool, token string) (Session, error) {
	row := pool.QueryRow(ctx, `SELECT `+sessionColumns+` FROM admin_sessions WHERE token_hash = $1`, hashToken(token))
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, errSessionNotFound
	}
	return sess, err
}

// lookupActiveSession implements RequireSession's three-condition check
// (技術架構設計第9節): status='active' AND now()<expires_at AND
// now()-last_seen_at<30min, all three required. Any failure deletes the
// row and reports "not authenticated" rather than surfacing which
// condition failed. Success touches last_seen_at.
func lookupActiveSession(ctx context.Context, pool *store.Pool, token string) (Session, bool, error) {
	sess, err := getSessionByToken(ctx, pool, token)
	if errors.Is(err, errSessionNotFound) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}

	now := time.Now()
	valid := sess.Status == statusActive && now.Before(sess.ExpiresAt) && now.Sub(sess.LastSeenAt) < idleTimeout
	if !valid {
		_ = deleteSession(ctx, pool, sess.ID)
		return Session{}, false, nil
	}

	if err := touchSession(ctx, pool, sess.ID); err != nil {
		return Session{}, false, err
	}
	sess.LastSeenAt = now
	return sess, true, nil
}

func touchSession(ctx context.Context, pool *store.Pool, id int64) error {
	_, err := pool.Exec(ctx, `UPDATE admin_sessions SET last_seen_at = now() WHERE id = $1`, id)
	return err
}

// activateSession implements 技術架構設計第9節步驟3: TOTP verified,
// pending_2fa -> active with the 12-hour absolute expiry and a fresh
// last_totp_verified_at (this login itself counts as the freshest
// possible TOTP verification).
func activateSession(ctx context.Context, pool *store.Pool, id int64) error {
	_, err := pool.Exec(ctx, `
		UPDATE admin_sessions
		SET status = $2, expires_at = now() + interval '12 hours', last_seen_at = now(), last_totp_verified_at = now(), pending_totp_secret = NULL
		WHERE id = $1
	`, id, statusActive)
	return err
}

func setPendingTOTPSecret(ctx context.Context, pool *store.Pool, id int64, secret string) error {
	_, err := pool.Exec(ctx, `UPDATE admin_sessions SET pending_totp_secret = $2 WHERE id = $1`, id, secret)
	return err
}

func markTOTPVerified(ctx context.Context, pool *store.Pool, id int64) error {
	_, err := pool.Exec(ctx, `UPDATE admin_sessions SET last_totp_verified_at = now() WHERE id = $1`, id)
	return err
}

func deleteSession(ctx context.Context, pool *store.Pool, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM admin_sessions WHERE id = $1`, id)
	return err
}

// deleteAllSessions clears every session — safe because the system has
// exactly one admin_account, so admin_sessions has no account_id column
// to scope by (技術架構設計第2節 table definition). Used on password change
// (技術架構設計第9節「Session模型」).
func deleteAllSessions(ctx context.Context, pool *store.Pool) error {
	_, err := pool.Exec(ctx, `DELETE FROM admin_sessions`)
	return err
}

// incrementTOTPAttempt/resetTOTPAttempt implement per-session brute-force
// tracking (技術架構設計第9節「登入限流」第2層). incrementTOTPAttempt returns
// the new count so the caller can decide whether to delete the session.
func incrementTOTPAttempt(ctx context.Context, pool *store.Pool, id int64) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `UPDATE admin_sessions SET totp_attempt_count = totp_attempt_count + 1 WHERE id = $1 RETURNING totp_attempt_count`, id).Scan(&count)
	return count, err
}

func resetTOTPAttempt(ctx context.Context, pool *store.Pool, id int64) error {
	_, err := pool.Exec(ctx, `UPDATE admin_sessions SET totp_attempt_count = 0 WHERE id = $1`, id)
	return err
}

func setSessionCookie(w http.ResponseWriter, token string, secure bool, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is parameterized by design (COOKIE_SECURE env var, 技術架構設計第9節「Session模型」) — HttpOnly/SameSite are always set below regardless
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(maxAge),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is parameterized by design (COOKIE_SECURE env var, 技術架構設計第9節「Session模型」) — HttpOnly/SameSite are always set below regardless
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func sessionTokenFromRequest(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	return c.Value, true
}
