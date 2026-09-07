package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

type contextKey int

const sessionContextKey contextKey = iota

// SessionFromContext retrieves the Session that RequireSession attached to
// the request context. Handlers wrapped by RequireSession can rely on this
// always succeeding.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(Session)
	return s, ok
}

// RequireSession implements 技術架構設計第9節「Session模型」: the
// status='active' AND not-expired AND not-idle three-condition check, plus
// the Origin-header CSRF check bundled into this same middleware (第9節
// 「CSRF防護」). pending_2fa sessions are rejected here by construction —
// lookupActiveSession only accepts status='active' — matching 第9節's
// explicit requirement that pending_2fa sessions can't reach any
// /admin/... route besides the login flow itself (which doesn't use this
// middleware).
func RequireSession(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" && origin != deps.PublicOrigin {
				http.Error(w, "origin mismatch", http.StatusForbidden)
				return
			}

			token, ok := sessionTokenFromRequest(r)
			if !ok {
				redirectToLogin(w, r)
				return
			}

			sess, ok, err := lookupActiveSession(r.Context(), deps.Pool, token)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				clearSessionCookie(w, deps.CookieSecure)
				redirectToLogin(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SelfPath returns the request's own URI (path+query). Pass this as
// RequireTOTPCode's returnTo for routes that are themselves a GET page —
// re-GETting the same URL after step-up verification is always safe.
func SelfPath(r *http.Request) string { return r.URL.RequestURI() }

// totpCodeFromRequest reads the step-up code a protected request carries.
// Native <form> submissions send it as an ordinary field; fetch()-based
// JSON requests (which have no PostForm) send it as a header instead —
// checking the header first lets a request use either without the caller
// needing to know which.
func totpCodeFromRequest(r *http.Request) string {
	if code := r.Header.Get("X-Totp-Code"); code != "" {
		return code
	}
	return r.PostFormValue("totp_code")
}

// appendTOTPErrorFlag adds a marker query param so returnTo's landing page
// can explain why the operation didn't go through — mirrors
// appendReverifiedFlag in handlers_reverify.go.
func appendTOTPErrorFlag(path, reason string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "totp_error=" + reason
}

// RequireTOTPCode implements 技術架構設計第9節「高風險操作Step-up驗證」
// ([ADR-0016](../../docs/adr/0016-高風險操作每次要求TOTP不設新鮮度窗口.md) 推翻ADR-0011
// 原訂的15分鐘新鮮度窗口): every high-risk request requires a currently-valid
// TOTP code, checked fresh every single time — there is no grace window.
// Must be composed after RequireSession (it reads the session from
// context, not the cookie).
//
//   - GET requests have no body to carry a code in, so they redirect to the
//     standalone /admin/reverify-totp challenge page first — returnTo(r) is
//     where reverify sends the operator back to once they enter a code.
//     The one exception is reverifyRedirectGrace's short window covering
//     that exact redirect hop back (see its doc comment) — without it,
//     landing back on this same GET route immediately after a successful
//     reverify would just challenge again, forever.
//   - POST/DELETE requests carry their own totp_code field/header (see
//     totpCodeFromRequest) alongside whatever data the request is actually
//     submitting, so a valid code completes the action in the very same
//     request — no redirect, nothing lost. A missing/wrong/replayed code
//     redirects to returnTo(r) with a totp_error= flag explaining why,
//     exactly like the GET case; returnTo must always resolve to a
//     registered GET route, never to the protected POST/DELETE action
//     itself (a 303 redirect always replays as a browser GET regardless of
//     the original method — pointing it at a POST-only route 404/405s;
//     found 2026-09-07 testing POST /admin/params/tolerance, see
//     docs/進度.md).
func RequireTOTPCode(deps Deps, returnTo func(*http.Request) string) func(http.Handler) http.Handler {
	return requireTOTPCode(deps, returnTo, false)
}

// RequireTOTPCodeOrRecentStepUp is RequireTOTPCode's batch-friendly sibling
// for the 4 /tron-proxy/{consolidation,fee-topup}/{prepare,broadcast}
// endpoints, which page.js calls twice per item while processing a manual
// consolidation batch (見ADR-0016「批次寬限」修訂). Requiring a fresh code on
// every single one of those calls would mean typing 2N codes to process an
// N-item batch — unworkable. The first call in a batch-processing run still
// needs a fresh, non-replayed totp_code exactly like RequireTOTPCode; once
// that succeeds it refreshes last_totp_verified_at, and subsequent calls
// within batchStepUpWindow are let through without a code at all. This is
// deliberately narrower than ADR-0011's old blanket 15-minute grace window:
// it only applies to these 4 routes (never GET routes, never the other 9
// RequireTOTPCode-gated routes), and one valid code is still required to
// ever start.
func RequireTOTPCodeOrRecentStepUp(deps Deps, returnTo func(*http.Request) string) func(http.Handler) http.Handler {
	return requireTOTPCode(deps, returnTo, true)
}

// batchStepUpWindow bounds RequireTOTPCodeOrRecentStepUp's grace period —
// long enough to derive keys and sign/broadcast a realistically large
// manual-consolidation batch item by item, short enough that it isn't a
// return to ADR-0011's old always-on 15-minute window (that applied to 13
// routes including ones an operator might not touch again for hours; this
// applies to 4 routes only for the duration of one batch run).
const batchStepUpWindow = 15 * time.Minute

// reverifyRedirectGrace fixes a real redirect loop found 2026-09-08 testing
// GET /admin/password: reverifySubmitHandler's success path 303-redirects
// back to the originally-requested GET page (next), but that page is
// itself RequireTOTPCode-protected — if the GET branch below redirected to
// /admin/reverify-totp completely unconditionally, landing back on it
// immediately after a successful reverify would just bounce the operator
// into reverify-totp again, forever. This window exists ONLY to let that
// one redirect hop land — it is not a general "browse the page freely for
// N minutes" grace period like ADR-0011's old one: 30 seconds is enough to
// cover the redirect + page render, not enough to mean anything if the
// operator navigates away and back later (a fresh visit at that point
// challenges again, matching ADR-0016's "every real visit" intent).
const reverifyRedirectGrace = 30 * time.Second

func requireTOTPCode(deps Deps, returnTo func(*http.Request) string, allowRecentStepUp bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := SessionFromContext(r.Context())
			if !ok {
				http.Error(w, "internal error: RequireTOTPCode used without RequireSession", http.StatusInternalServerError)
				return
			}

			if r.Method == http.MethodGet {
				if sess.LastStepUpVerifiedAt != nil && time.Since(*sess.LastStepUpVerifiedAt) < reverifyRedirectGrace {
					next.ServeHTTP(w, r)
					return
				}
				target := "/admin/reverify-totp?next=" + url.QueryEscape(returnTo(r))
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}

			ctx := r.Context()
			code := totpCodeFromRequest(r)
			if code == "" {
				if allowRecentStepUp && sess.LastStepUpVerifiedAt != nil && time.Since(*sess.LastStepUpVerifiedAt) < batchStepUpWindow {
					next.ServeHTTP(w, r)
					return
				}
				http.Redirect(w, r, appendTOTPErrorFlag(returnTo(r), "missing"), http.StatusSeeOther)
				return
			}

			account, err := loadAccount(ctx, deps.Pool)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if account.TOTPSecret == nil {
				// Can't happen: reaching an active session already implies 2FA
				// setup completed. Fail loudly rather than silently
				// mis-redirecting (mirrors reverifySubmitHandler's same guard).
				http.Error(w, "internal error: active session with no totp_secret", http.StatusInternalServerError)
				return
			}

			ip := clientIP(r)
			if !sharedIPLimiter.allow(ip) {
				http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
				return
			}

			step, ok := verifyTOTPCode(*account.TOTPSecret, code, time.Now())
			if !ok {
				locked, ferr := handleTOTPFailure(ctx, deps, sharedIPLimiter, ip, sess, "TOTP_STEPUP_FAILED")
				if ferr != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				if locked {
					clearSessionCookie(w, deps.CookieSecure)
					http.Redirect(w, r, "/admin/login?reason=locked", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, appendTOTPErrorFlag(returnTo(r), "invalid"), http.StatusSeeOther)
				return
			}
			if isReplay(account, step) {
				http.Redirect(w, r, appendTOTPErrorFlag(returnTo(r), "replay"), http.StatusSeeOther)
				return
			}

			if err := completeReverify(ctx, deps, account, sess, step); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			_ = audit.Log(ctx, deps.Pool, "admin", "TOTP_STEPUP_SUCCESS", nil, nil, map[string]any{"ip": ip})

			next.ServeHTTP(w, r)
		})
	}
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
