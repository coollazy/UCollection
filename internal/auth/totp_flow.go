package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

// totpAttemptLimit implements 技術架構設計第9節「登入限流」第2層: 5 consecutive
// failures (across setup/login/reverify) invalidates the session outright.
const totpAttemptLimit = 5

var errNoPendingSession = errors.New("auth: no valid pending_2fa session")

// loadPendingSession reads the session cookie and requires a still-valid
// pending_2fa row — used by the totp-setup/totp handlers, which are
// explicitly excluded from RequireSession (技術架構設計第9節). A session
// found in the wrong status (e.g. already 'active') is left untouched
// rather than deleted — it may be a legitimate active session the caller
// reached this URL by mistake, and nuking someone's valid login as a side
// effect of a wrong click would be a bad surprise.
func loadPendingSession(ctx context.Context, deps Deps, r *http.Request) (Session, error) {
	token, ok := sessionTokenFromRequest(r)
	if !ok {
		return Session{}, errNoPendingSession
	}
	sess, err := getSessionByToken(ctx, deps.Pool, token)
	if errors.Is(err, errSessionNotFound) {
		return Session{}, errNoPendingSession
	}
	if err != nil {
		return Session{}, err
	}
	if sess.Status != statusPendingTOTP {
		return Session{}, errNoPendingSession
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = deleteSession(ctx, deps.Pool, sess.ID)
		return Session{}, errNoPendingSession
	}
	return sess, nil
}

func isReplay(account Account, step int64) bool {
	return account.LastTOTPStep != nil && step <= *account.LastTOTPStep
}

// handleTOTPFailure records one wrong-code attempt against both rate
// limiter layers (技術架構設計第9節「登入限流」). locked reports whether this
// attempt was the 5th consecutive failure, in which case the session has
// already been deleted and the caller should redirect to /admin/login
// rather than re-render the current form.
func handleTOTPFailure(ctx context.Context, deps Deps, limiter *ipLimiter, ip string, sess Session, failedActionType string) (locked bool, err error) {
	limiter.recordFailure(ip)

	count, err := incrementTOTPAttempt(ctx, deps.Pool, sess.ID)
	if err != nil {
		return false, err
	}
	if count >= totpAttemptLimit {
		if err := deleteSession(ctx, deps.Pool, sess.ID); err != nil {
			return false, err
		}
		_ = audit.Log(ctx, deps.Pool, "admin", "SESSION_LOCKED", nil, nil, map[string]any{"ip": ip})
		return true, nil
	}

	_ = audit.Log(ctx, deps.Pool, "admin", failedActionType, nil, nil, map[string]any{"ip": ip})
	return false, nil
}

// completeTOTPStep implements the shared success path for the two-stage
// login (setup or verify, pending_2fa -> active, 技術架構設計第9節步驟2/3):
// record the matched time-step for replay protection, activate the
// session (12-hour absolute expiry, fresh last_totp_verified_at), and
// reset the consecutive failure counter.
func completeTOTPStep(ctx context.Context, deps Deps, account Account, sess Session, step int64) error {
	if err := updateLastTOTPStep(ctx, deps.Pool, account.ID, step); err != nil {
		return err
	}
	if err := activateSession(ctx, deps.Pool, sess.ID); err != nil {
		return err
	}
	return resetTOTPAttempt(ctx, deps.Pool, sess.ID)
}

// completeReverify implements the step-up success path (技術架構設計第9節
// 「高風險操作Step-up驗證」): unlike completeTOTPStep, it must NOT touch
// expires_at — the 12-hour absolute session cap is deliberately unaffected
// by activity, reverify only refreshes last_totp_verified_at.
func completeReverify(ctx context.Context, deps Deps, account Account, sess Session, step int64) error {
	if err := updateLastTOTPStep(ctx, deps.Pool, account.ID, step); err != nil {
		return err
	}
	if err := markTOTPVerified(ctx, deps.Pool, sess.ID); err != nil {
		return err
	}
	return resetTOTPAttempt(ctx, deps.Pool, sess.ID)
}
