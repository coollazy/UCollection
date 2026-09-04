package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreatePendingSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, err := createPendingSession(ctx, pool)
	if err != nil {
		t.Fatalf("createPendingSession() error = %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if sess.Status != statusPendingTOTP {
		t.Fatalf("Status = %q, want pending_2fa", sess.Status)
	}
	if sess.TOTPAttemptCount != 0 {
		t.Fatalf("TOTPAttemptCount = %d, want 0", sess.TOTPAttemptCount)
	}
	if time.Until(sess.ExpiresAt) > pendingSessionTTL || time.Until(sess.ExpiresAt) < pendingSessionTTL-time.Minute {
		t.Fatalf("ExpiresAt = %v, want ~%v from now", sess.ExpiresAt, pendingSessionTTL)
	}

	got, err := getSessionByToken(ctx, pool, token)
	if err != nil {
		t.Fatalf("getSessionByToken() error = %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("getSessionByToken().ID = %d, want %d", got.ID, sess.ID)
	}
}

func TestGetSessionByToken_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	if _, err := getSessionByToken(context.Background(), pool, "no-such-token"); err != errSessionNotFound {
		t.Fatalf("getSessionByToken() error = %v, want errSessionNotFound", err)
	}
}

func TestActivateSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, err := createPendingSession(ctx, pool)
	if err != nil {
		t.Fatalf("createPendingSession() error = %v", err)
	}
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}

	got, err := getSessionByToken(ctx, pool, token)
	if err != nil {
		t.Fatalf("getSessionByToken() error = %v", err)
	}
	if got.Status != statusActive {
		t.Fatalf("Status = %q, want active", got.Status)
	}
	if got.LastTOTPVerifiedAt == nil {
		t.Fatal("LastTOTPVerifiedAt is nil after activation")
	}
	if time.Until(got.ExpiresAt) > activeSessionTTL || time.Until(got.ExpiresAt) < activeSessionTTL-time.Minute {
		t.Fatalf("ExpiresAt = %v, want ~%v from now", got.ExpiresAt, activeSessionTTL)
	}
}

func TestLookupActiveSession_ValidSessionPasses(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, _ := createPendingSession(ctx, pool)
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}

	got, ok, err := lookupActiveSession(ctx, pool, token)
	if err != nil {
		t.Fatalf("lookupActiveSession() error = %v", err)
	}
	if !ok {
		t.Fatal("lookupActiveSession() ok = false for a freshly activated session")
	}
	if got.ID != sess.ID {
		t.Fatalf("got.ID = %d, want %d", got.ID, sess.ID)
	}
}

func TestLookupActiveSession_PendingStatusRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, _, _ := createPendingSession(ctx, pool)

	_, ok, err := lookupActiveSession(ctx, pool, token)
	if err != nil {
		t.Fatalf("lookupActiveSession() error = %v", err)
	}
	if ok {
		t.Fatal("lookupActiveSession() ok = true for a pending_2fa session, want false")
	}
}

func TestLookupActiveSession_ExpiredDeletesRow(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, _ := createPendingSession(ctx, pool)
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET expires_at = now() - interval '1 second' WHERE id = $1`, sess.ID); err != nil {
		t.Fatalf("force expiry: %v", err)
	}

	_, ok, err := lookupActiveSession(ctx, pool, token)
	if err != nil {
		t.Fatalf("lookupActiveSession() error = %v", err)
	}
	if ok {
		t.Fatal("lookupActiveSession() ok = true for an expired session, want false")
	}

	if _, err := getSessionByToken(ctx, pool, token); err != errSessionNotFound {
		t.Fatalf("expired session row not deleted: getSessionByToken() error = %v", err)
	}
}

func TestLookupActiveSession_IdleTimeoutDeletesRow(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, _ := createPendingSession(ctx, pool)
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET last_seen_at = now() - interval '31 minutes' WHERE id = $1`, sess.ID); err != nil {
		t.Fatalf("force idle: %v", err)
	}

	_, ok, err := lookupActiveSession(ctx, pool, token)
	if err != nil {
		t.Fatalf("lookupActiveSession() error = %v", err)
	}
	if ok {
		t.Fatal("lookupActiveSession() ok = true past the 30-minute idle timeout, want false")
	}
}

func TestLookupActiveSession_TouchesLastSeenAt(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	token, sess, _ := createPendingSession(ctx, pool)
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET last_seen_at = now() - interval '5 minutes' WHERE id = $1`, sess.ID); err != nil {
		t.Fatalf("age last_seen_at: %v", err)
	}

	before := time.Now().Add(-time.Minute)
	if _, ok, err := lookupActiveSession(ctx, pool, token); err != nil || !ok {
		t.Fatalf("lookupActiveSession() ok=%v err=%v", ok, err)
	}

	got, err := getSessionByToken(ctx, pool, token)
	if err != nil {
		t.Fatalf("getSessionByToken() error = %v", err)
	}
	if got.LastSeenAt.Before(before) {
		t.Fatalf("LastSeenAt = %v, want updated to ~now", got.LastSeenAt)
	}
}

func TestDeleteAllSessions(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, _, err := createPendingSession(ctx, pool); err != nil {
			t.Fatalf("createPendingSession() error = %v", err)
		}
	}
	if err := deleteAllSessions(ctx, pool); err != nil {
		t.Fatalf("deleteAllSessions() error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_sessions`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("admin_sessions count = %d, want 0", count)
	}
}

func TestTOTPAttemptCounter(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	_, sess, _ := createPendingSession(ctx, pool)

	for want := 1; want <= 3; want++ {
		got, err := incrementTOTPAttempt(ctx, pool, sess.ID)
		if err != nil {
			t.Fatalf("incrementTOTPAttempt() error = %v", err)
		}
		if got != want {
			t.Fatalf("incrementTOTPAttempt() = %d, want %d", got, want)
		}
	}

	if err := resetTOTPAttempt(ctx, pool, sess.ID); err != nil {
		t.Fatalf("resetTOTPAttempt() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT totp_attempt_count FROM admin_sessions WHERE id = $1`, sess.ID).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("totp_attempt_count = %d, want 0 after reset", count)
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	setSessionCookie(rec, "abc123", true, activeSessionTTL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	token, ok := sessionTokenFromRequest(req)
	if !ok || token != "abc123" {
		t.Fatalf("sessionTokenFromRequest() = (%q, %v), want (abc123, true)", token, ok)
	}

	rec2 := httptest.NewRecorder()
	clearSessionCookie(rec2, true)
	cookies := rec2.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatal("clearSessionCookie() did not produce an expiring cookie")
	}
}
