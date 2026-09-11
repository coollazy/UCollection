package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

// TestRequireTOTPCode_GETAlwaysRedirectsToReverify ADR-0016: GET-protected
// pages have no body to carry a code in, so every visit — no exception for
// a just-activated session — bounces through /admin/reverify-totp first.
func TestRequireTOTPCode_GETAlwaysRedirectsToReverify(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	chain := func() http.Handler { return RequireSession(deps)(RequireTOTPCode(deps, SelfPath)(okHandler())) }

	t.Run("just activated", func(t *testing.T) {
		token := activeSessionToken(t, deps) // activateSession sets last_totp_verified_at=now()
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/admin/password", nil), token)
		chain().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 redirect to reverify even for a just-activated session", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if loc == "" || loc[:len("/admin/reverify-totp")] != "/admin/reverify-totp" {
			t.Fatalf("Location = %q, want it to start with /admin/reverify-totp", loc)
		}
	})

	t.Run("long inactive", func(t *testing.T) {
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

// activeSessionWithTOTPSecret bootstraps an admin_account row with a known
// TOTP secret and an active session bound to it — the setup POST/DELETE
// routes need, since RequireTOTPCode verifies a real code against the
// account (unlike the old freshness-only check).
func activeSessionWithTOTPSecret(t *testing.T, deps Deps) (token, secret string) {
	t.Helper()
	ctx := context.Background()
	if err := EnsureAdminAccount(ctx, deps.Pool, testUsername, testPassword); err != nil {
		t.Fatalf("EnsureAdminAccount() error = %v", err)
	}
	account, err := loadAccount(ctx, deps.Pool)
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	secret, err = newTOTPSecret(testUsername)
	if err != nil {
		t.Fatalf("newTOTPSecret() error = %v", err)
	}
	if _, err := deps.Pool.Exec(ctx, `UPDATE admin_account SET totp_secret = $1 WHERE id = $2`, secret, account.ID); err != nil {
		t.Fatalf("preset totp_secret: %v", err)
	}
	return activeSessionToken(t, deps), secret
}

// TestRequireTOTPCode_POST 2026-09-07迴歸測試組（ADR-0016）：POST/DELETE的
// totp_code欄位內建在同一次請求裡，缺漏/錯誤/重放時導回returnTo(r)並附上
// totp_error=旗標（不是導向/admin/reverify-totp——那是給GET請求用的），驗證通過
// 則同一次請求內直接放行、不轉址。這也是2026-09-07實測發現的405 bug的迴歸測試：
// 舊版把next設成被攔截的原始請求本身，reverify成功後303轉址回POST-only端點會變
// 成GET而405；新機制完全不做「轉址回原端點」這件事，從根本上不會再踩到。
func TestRequireTOTPCode_POST(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	returnTo := func(*http.Request) string { return "/admin/params" }
	chain := func() http.Handler { return RequireSession(deps)(RequireTOTPCode(deps, returnTo)(okHandler())) }

	t.Run("missing code redirects to returnTo with totp_error=missing", func(t *testing.T) {
		token, _ := activeSessionWithTOTPSecret(t, deps)
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/params/tolerance", nil), token)
		chain().ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse Location: %v", err)
		}
		if loc.Path != "/admin/params" || loc.Query().Get("totp_error") != "missing" {
			t.Fatalf("Location = %q, want /admin/params?totp_error=missing (the returnTo GET page, not the original POST-only request URI /admin/params/tolerance)", rec.Header().Get("Location"))
		}
	})

	t.Run("wrong code redirects to returnTo with totp_error=invalid", func(t *testing.T) {
		token, _ := activeSessionWithTOTPSecret(t, deps)
		form := url.Values{"totp_code": {"000000"}}
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/params/tolerance", strings.NewReader(form.Encode())), token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		chain().ServeHTTP(rec, req)

		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse Location: %v", err)
		}
		if rec.Code != http.StatusSeeOther || loc.Query().Get("totp_error") != "invalid" {
			t.Fatalf("status=%d location=%q, want 303 to ...?totp_error=invalid", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("valid code completes in the same request, no redirect", func(t *testing.T) {
		token, secret := activeSessionWithTOTPSecret(t, deps)
		form := url.Values{"totp_code": {codeAt(t, secret, time.Now())}}
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/params/tolerance", strings.NewReader(form.Encode())), token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		chain().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 — a valid code should reach the protected handler directly, no redirect (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid code via X-Totp-Code header also completes (fetch-based routes)", func(t *testing.T) {
		token, secret := activeSessionWithTOTPSecret(t, deps)
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/master-wallets", nil), token)
		// +30s: a different time-step than the previous subtest's code, since
		// last_totp_step is account-level and both subtests can land in the
		// same 30-second step when run back-to-back (would otherwise look
		// like code replay even with a freshly-regenerated secret).
		req.Header.Set("X-Totp-Code", codeAt(t, secret, time.Now().Add(30*time.Second)))
		chain().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a valid code sent via header (body: %s)", rec.Code, rec.Body.String())
		}
	})
}

// TestRequireTOTPCodeOrRecentStepUp_BatchGrace covers ADR-0016's「批次寬限」
// amendment: the 4 /tron-proxy/... endpoints let a session skip the code on
// requests shortly after a prior request in the same session verified one,
// so a page.js batch run only prompts once instead of twice per item.
func TestRequireTOTPCodeOrRecentStepUp_BatchGrace(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := testDeps(pool)
	returnTo := func(*http.Request) string { return "/admin/consolidation" }
	chain := func() http.Handler {
		return RequireSession(deps)(RequireTOTPCodeOrRecentStepUp(deps, returnTo)(okHandler()))
	}

	t.Run("no prior step-up and no code redirects with totp_error=missing", func(t *testing.T) {
		token := activeSessionToken(t, deps)
		rec := httptest.NewRecorder()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/tron-proxy/consolidation/prepare", nil), token)
		chain().ServeHTTP(rec, req)

		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse Location: %v", err)
		}
		if rec.Code != http.StatusSeeOther || loc.Query().Get("totp_error") != "missing" {
			t.Fatalf("status=%d location=%q, want 303 to ...?totp_error=missing", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("first call needs a code, later calls in the same session don't", func(t *testing.T) {
		token, secret := activeSessionWithTOTPSecret(t, deps)

		firstRec := httptest.NewRecorder()
		firstReq := withCookie(httptest.NewRequest(http.MethodPost, "/tron-proxy/consolidation/prepare", nil), token)
		firstReq.Header.Set("X-Totp-Code", codeAt(t, secret, time.Now()))
		chain().ServeHTTP(firstRec, firstReq)
		if firstRec.Code != http.StatusOK {
			t.Fatalf("first call: status = %d, want 200 (body: %s)", firstRec.Code, firstRec.Body.String())
		}

		// No X-Totp-Code header at all this time — must still pass, riding
		// the batch grace window the first call just established.
		secondRec := httptest.NewRecorder()
		secondReq := withCookie(httptest.NewRequest(http.MethodPost, "/tron-proxy/consolidation/broadcast", nil), token)
		chain().ServeHTTP(secondRec, secondReq)
		if secondRec.Code != http.StatusOK {
			t.Fatalf("second call (no code, within grace window): status = %d, want 200 (body: %s)", secondRec.Code, secondRec.Body.String())
		}
	})

	// GET doesn't ride RequireTOTPCodeOrRecentStepUp's 30-minute batch grace
	// window — it only ever gets reverifyRedirectGrace's much shorter
	// exemption (30s, just enough to cover reverifySubmitHandler's own
	// redirect hop back to the GET page it originally challenged — see
	// that constant's doc comment for the loop it fixes). Both edges of
	// that distinction share a single priming validation (only 2 non-
	// replayed codes are obtainable within one fast-running test — see
	// totpCodeSequence's doc comment in prepare_broadcast_test.go for the
	// same ±1-period-skew constraint), so this is one subtest, not two.
	t.Run("GET rides reverifyRedirectGrace briefly, not the longer batch window", func(t *testing.T) {
		token, secret := activeSessionWithTOTPSecret(t, deps)
		primeRec := httptest.NewRecorder()
		primeReq := withCookie(httptest.NewRequest(http.MethodPost, "/tron-proxy/consolidation/prepare", nil), token)
		// +30s: last_totp_step is account-level and the previous subtest's
		// step could still be "now"'s step if run back-to-back — see the
		// same pattern's comment on the header subtest above.
		primeReq.Header.Set("X-Totp-Code", codeAt(t, secret, time.Now().Add(30*time.Second)))
		chain().ServeHTTP(primeRec, primeReq)
		if primeRec.Code != http.StatusOK {
			t.Fatalf("priming call: status = %d, want 200", primeRec.Code)
		}

		getRec := httptest.NewRecorder()
		getReq := withCookie(httptest.NewRequest(http.MethodGet, "/admin/consolidation/sign", nil), token)
		chain().ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("GET immediately after step-up: status = %d, want 200 (reverifyRedirectGrace should cover this)", getRec.Code)
		}

		// Backdate past reverifyRedirectGrace (30s) but still well within
		// batchStepUpWindow (30min) — simulates the operator having
		// stepped up a minute ago, then separately navigating to a
		// GET-protected page; must challenge again, not silently pass.
		if _, err := pool.Exec(context.Background(), `UPDATE admin_sessions SET last_stepup_verified_at = now() - interval '1 minute' WHERE token_hash = $1`, hashToken(token)); err != nil {
			t.Fatalf("backdate last_stepup_verified_at: %v", err)
		}

		getRec2 := httptest.NewRecorder()
		getReq2 := withCookie(httptest.NewRequest(http.MethodGet, "/admin/consolidation/sign", nil), token)
		chain().ServeHTTP(getRec2, getReq2)
		if getRec2.Code != http.StatusSeeOther {
			t.Fatalf("GET a minute after step-up: status = %d, want 303 to reverify (batchStepUpWindow must not leak into GET routes)", getRec2.Code)
		}
	})
}
