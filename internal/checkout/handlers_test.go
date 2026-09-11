package checkout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
)

func doGET(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestPage_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)

	for _, token := range []string{"does-not-exist", "!!!not-a-token!!!", ""} {
		rec := doGET(t, mux, "/checkout/"+token)
		// Empty token routes to /checkout/ which the pattern doesn't match → 404 too.
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /checkout/%q = %d, want 404", token, rec.Code)
		}
	}
}

func TestPage_Pending(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)
	o := newPendingOrder(t, pool, "order-pending")

	rec := doGET(t, mux, "/checkout/"+o.PublicToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	wantContains := []string{
		"data:image/png;base64,", // QR as data URI
		o.Address,                // receiving address
		"100 USDT",               // target amount only
		`data-countdown="true"`,
		`data-terminal="false"`,
		`data-status-url="/checkout/` + o.PublicToken + `/status"`,
		"等待付款",
		`<meta name="robots" content="noindex,nofollow">`,
		"/static/js/checkout.js",
	}
	for _, s := range wantContains {
		if !strings.Contains(body, s) {
			t.Errorf("page body missing %q", s)
		}
	}
	// remaining must be a positive number (order just created, 900s validity).
	if strings.Contains(body, `data-remaining="0"`) {
		t.Error("PENDING order should have a positive data-remaining, got 0")
	}
	// The tolerance bounds (98.5 / 101.5 USDT) must never leak to the page.
	for _, leak := range []string{"98.5", "101.5"} {
		if strings.Contains(body, leak) {
			t.Errorf("page body leaked tolerance bound %q", leak)
		}
	}
}

func TestPage_Confirming(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)
	o := newPendingOrder(t, pool, "order-confirming")
	setStatus(t, pool, o.ID, order.StatusConfirming)

	rec := doGET(t, mux, "/checkout/"+o.PublicToken)
	body := rec.Body.String()
	if !strings.Contains(body, `data-countdown="false"`) {
		t.Error("CONFIRMING must not show a countdown")
	}
	if !strings.Contains(body, `data-terminal="false"`) {
		t.Error("CONFIRMING is not terminal")
	}
	if !strings.Contains(body, "確認中") {
		t.Error("CONFIRMING should show a no-time-pressure confirming message")
	}
}

func TestPage_TerminalStates(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)

	details := map[order.Status]string{}
	for i, st := range []order.Status{
		order.StatusCompleted, order.StatusOverpaid,
		order.StatusConfirmationStalled, order.StatusExpired,
	} {
		o := newPendingOrder(t, pool, "order-term-"+string(rune('a'+i)))
		setStatus(t, pool, o.ID, st)
		rec := doGET(t, mux, "/checkout/"+o.PublicToken)
		body := rec.Body.String()
		if !strings.Contains(body, `data-terminal="true"`) {
			t.Errorf("%s should be terminal", st)
		}
		if !strings.Contains(body, `data-countdown="false"`) {
			t.Errorf("%s must not show a countdown", st)
		}
		details[st] = body
	}
	// CONFIRMATION_STALLED and EXPIRED must not share wording (第6節).
	if strings.Contains(details[order.StatusConfirmationStalled], "有效期限內未收到足額款項") {
		t.Error("STALLED body must not reuse EXPIRED's wording")
	}
	if strings.Contains(details[order.StatusExpired], "確認時限內未完成鏈上確認") {
		t.Error("EXPIRED body must not reuse STALLED's wording")
	}
}

func TestStatusFragment(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)
	o := newPendingOrder(t, pool, "order-fragment")

	rec := doGET(t, mux, "/checkout/"+o.PublicToken+"/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="status-region"`) {
		t.Error("fragment must contain #status-region")
	}
	// It's a fragment, not a full page.
	if strings.Contains(body, "<html") || strings.Contains(body, "<!doctype") {
		t.Error("status fragment must not be a full HTML document")
	}
}

func TestStatusFragment_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)

	rec := doGET(t, mux, "/checkout/nope/status")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mux := testMux(pool)
	o := newPendingOrder(t, pool, "order-headers")

	rec := doGET(t, mux, "/checkout/"+o.PublicToken)
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"img-src 'self' data:", "script-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
