package consolidation

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var pageDataRe = regexp.MustCompile(`(?s)<div id="page-data" hidden>(.*?)</div>`)

// extractPageData pulls signPageJSON back out of the rendered sign page —
// mirrors what sign-entry.js will do in the browser (read #page-data's
// textContent, JSON.parse it), except here we also have to undo the plain
// HTML-escaping html/template applied (see templates/consolidation_sign.html).
func extractPageData(t *testing.T, body string) signPageJSON {
	t.Helper()
	m := pageDataRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("page-data div not found in body: %s", body)
	}
	var page signPageJSON
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &page); err != nil {
		t.Fatalf("unmarshal page-data: %v (raw=%q)", err, m[1])
	}
	return page
}

func TestSignPageHandler_Consolidation(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	path := "/admin/consolidation/sign?" + url.Values{
		"type":     {"consolidation"},
		"batch_id": {strconv.FormatInt(batchID, 10)},
		"order_id": {strconv.FormatInt(ord.ID, 10)},
	}.Encode()
	resp := callSignPageHandler(t, deps, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != signCSPHeader {
		t.Fatalf("CSP header = %q, want %q", got, signCSPHeader)
	}

	page := extractPageData(t, readBody(t, resp))
	if page.Type != "consolidation" || page.BatchID != batchID || page.MasterWalletID != walletID {
		t.Fatalf("page = %+v, want type=consolidation batch_id=%d master_wallet_id=%d", page, batchID, walletID)
	}
	if page.Xpub != testXpub {
		t.Fatalf("Xpub = %q, want %q", page.Xpub, testXpub)
	}
	if page.DestinationAddress != testDestinationAddress {
		t.Fatalf("DestinationAddress = %q, want %q", page.DestinationAddress, testDestinationAddress)
	}
	if len(page.Items) != 1 || page.Items[0].OrderID != ord.ID || page.Items[0].Address != ord.Address || page.Items[0].DerivationIndex != ord.DerivationIndex {
		t.Fatalf("Items = %+v, want single item matching order %+v", page.Items, ord)
	}
}

func TestSignPageHandler_FeeTopup(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	batchID, err := CreateFeeTopupBatch(context.Background(), deps, walletID, "A1", testSourceAddress)
	if err != nil {
		t.Fatalf("CreateFeeTopupBatch() error = %v", err)
	}
	path := "/admin/consolidation/sign?" + url.Values{
		"type":             {"fee-topup"},
		"batch_id":         {strconv.FormatInt(batchID, 10)},
		"order_id":         {strconv.FormatInt(ord.ID, 10)},
		"amount_per_order": {"1000000"},
	}.Encode()
	resp := callSignPageHandler(t, deps, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, readBody(t, resp))
	}

	page := extractPageData(t, readBody(t, resp))
	if page.Type != "fee-topup" || page.BatchID != batchID || page.MasterWalletID != walletID {
		t.Fatalf("page = %+v, want type=fee-topup batch_id=%d master_wallet_id=%d", page, batchID, walletID)
	}
	if page.FeeSourceAddress != testSourceAddress {
		t.Fatalf("FeeSourceAddress = %q, want %q", page.FeeSourceAddress, testSourceAddress)
	}
	if page.FeeSource != "A1" {
		t.Fatalf("FeeSource = %q, want A1（第14項：sign 頁曾漏帶 fee_source 導致顯示 undefined）", page.FeeSource)
	}
	if page.DefaultAmountPerOrder != 1000000 {
		t.Fatalf("DefaultAmountPerOrder = %d, want 1000000", page.DefaultAmountPerOrder)
	}
	if page.Xpub != "" {
		t.Fatalf("Xpub = %q, want empty for fee-topup", page.Xpub)
	}
	if len(page.Items) != 1 || page.Items[0].OrderID != ord.ID {
		t.Fatalf("Items = %+v, want single item for order %d", page.Items, ord.ID)
	}
}

func TestSignPageHandler_InvalidType(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	resp := callSignPageHandler(t, deps, "/admin/consolidation/sign?type=bogus&batch_id=1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestSignPageHandler_BatchNotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}

	resp := callSignPageHandler(t, deps, "/admin/consolidation/sign?type=consolidation&batch_id=999999")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestSignPageHandler_SkipsOrderFromDifferentMasterWallet(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	otherWalletID := newMasterWallet(t, pool)
	// Same xpub as walletID (see newMasterWallet) — bump last_derived_index
	// so otherWalletID's order doesn't derive the same address as walletID's.
	if _, err := pool.Exec(context.Background(), `UPDATE master_wallets SET last_derived_index = 50 WHERE id = $1`, otherWalletID); err != nil {
		t.Fatalf("bump last_derived_index: %v", err)
	}
	ownOrder := newOrder(t, pool, walletID, "order-own")
	foreignOrder := newOrder(t, pool, otherWalletID, "order-foreign")
	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	path := "/admin/consolidation/sign?" + url.Values{
		"type":     {"consolidation"},
		"batch_id": {strconv.FormatInt(batchID, 10)},
		"order_id": {strconv.FormatInt(ownOrder.ID, 10), strconv.FormatInt(foreignOrder.ID, 10)},
	}.Encode()
	resp := callSignPageHandler(t, deps, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, readBody(t, resp))
	}

	page := extractPageData(t, readBody(t, resp))
	if len(page.Items) != 1 || page.Items[0].OrderID != ownOrder.ID {
		t.Fatalf("Items = %+v, want only the order belonging to master wallet %d", page.Items, walletID)
	}
}

// TestSignPageHandler_RequireFreshTOTP ADR-0016: GET-protected pages have
// no body to carry a code in, so every single visit — not just a stale
// one — bounces through /admin/reverify-totp first.
func TestSignPageHandler_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	path := "/admin/consolidation/sign?type=consolidation&batch_id=" + strconv.FormatInt(batchID, 10)
	resp := doGet(t, srv, cookie, path)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/reverify-totp") {
		t.Fatalf("status=%d location=%q, want 303 to reverify", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// callSignPageHandler exercises signPageHandler directly rather than
// through the full mux: ADR-0016 makes RequireTOTPCode unconditionally
// redirect every GET to /admin/reverify-totp first (no freshness grace
// window), so a request routed through the real mux never reaches this
// handler without a full login+TOTP-code round trip. The handler's own
// page-data/validation/CSP-header logic is unaffected by what gates it, so
// testing it in isolation is both simpler and still exercises the real
// behavior under test.
func callSignPageHandler(t *testing.T, deps Deps, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	signPageHandler(deps).ServeHTTP(rec, req)
	return rec.Result()
}
