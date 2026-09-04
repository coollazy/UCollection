package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withCookie(req *http.Request, token string) *http.Request {
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token}) //nolint:gosec // test-only outgoing request cookie, not a real Set-Cookie response
	return req
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SessionFromContext(r.Context()); !ok {
			http.Error(w, "no session in context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func activeSessionToken(t *testing.T, deps Deps) string {
	t.Helper()
	ctx := context.Background()
	token, sess, err := createPendingSession(ctx, deps.Pool)
	if err != nil {
		t.Fatalf("createPendingSession() error = %v", err)
	}
	if err := activateSession(ctx, deps.Pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}
	return token
}

func TestRequireSession_NoCookieRedirectsToLogin(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/whatever", nil)
	RequireSession(deps)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("status=%d location=%q, want 303 to /admin/login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequireSession_ValidSessionPassesThrough(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	token := activeSessionToken(t, deps)

	rec := httptest.NewRecorder()
	req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/whatever", nil), token)
	RequireSession(deps)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestRequireSession_PendingSessionRedirectsToLogin(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	token, _, err := createPendingSession(context.Background(), pool)
	if err != nil {
		t.Fatalf("createPendingSession() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/whatever", nil), token)
	RequireSession(deps)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("status=%d location=%q, want 303 to /admin/login for a pending_2fa session", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequireSession_ExpiredSessionClearsCookieAndRedirects(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	ctx := context.Background()
	token, sess, _ := createPendingSession(ctx, pool)
	if err := activateSession(ctx, pool, sess.ID); err != nil {
		t.Fatalf("activateSession() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET expires_at = now() - interval '1 second' WHERE id = $1`, sess.ID); err != nil {
		t.Fatalf("force expiry: %v", err)
	}

	rec := httptest.NewRecorder()
	req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/whatever", nil), token)
	RequireSession(deps)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatal("expired session did not clear the cookie")
	}
}

func TestRequireSession_OriginMismatchRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	token := activeSessionToken(t, deps)

	rec := httptest.NewRecorder()
	req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/whatever", nil), token)
	req.Header.Set("Origin", "https://evil.example")
	RequireSession(deps)(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a mismatched Origin header", rec.Code)
	}
}

func TestRequireSession_OriginMatchOrAbsentPasses(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)

	t.Run("matching origin", func(t *testing.T) {
		token := activeSessionToken(t, deps)
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/whatever", nil), token)
		req.Header.Set("Origin", deps.PublicOrigin)
		RequireSession(deps)(okHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a matching Origin header", rec.Code)
		}
	})

	t.Run("absent origin", func(t *testing.T) {
		token := activeSessionToken(t, deps)
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/whatever", nil), token)
		RequireSession(deps)(okHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 when Origin header is absent entirely", rec.Code)
		}
	})
}

func TestRequireFreshTOTP_FreshPassesStaleRedirects(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	chain := func() http.Handler { return RequireSession(deps)(RequireFreshTOTP(deps)(okHandler())) }

	t.Run("fresh", func(t *testing.T) {
		token := activeSessionToken(t, deps) // activateSession sets last_totp_verified_at=now()
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/password", nil), token)
		chain().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a freshly-verified session", rec.Code)
		}
	})

	t.Run("stale", func(t *testing.T) {
		ctx := context.Background()
		token, sess, _ := createPendingSession(ctx, pool)
		if err := activateSession(ctx, pool, sess.ID); err != nil {
			t.Fatalf("activateSession() error = %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE admin_sessions SET last_totp_verified_at = now() - interval '16 minutes' WHERE id = $1`, sess.ID); err != nil {
			t.Fatalf("age last_totp_verified_at: %v", err)
		}

		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/password", nil), token)
		chain().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 redirect to reverify", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if loc == "" || loc[:len("/admin/reverify-totp")] != "/admin/reverify-totp" {
			t.Fatalf("Location = %q, want it to start with /admin/reverify-totp", loc)
		}
	})
}
