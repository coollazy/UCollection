package consolidation

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCreateConsolidationBatchHandler_RedirectsToSignPage(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord1 := newOrder(t, pool, walletID, "order-1")
	ord2 := newOrder(t, pool, walletID, "order-2")
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/batches", url.Values{
		"master_wallet_id":    {strconv.FormatInt(walletID, 10)},
		"destination_address": {testDestinationAddress},
		"order_id":            {strconv.FormatInt(ord1.ID, 10), strconv.FormatInt(ord2.ID, 10)},
		"totp_code":           {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body = %s", resp.StatusCode, readBody(t, resp))
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/admin/consolidation/sign?") {
		t.Fatalf("Location = %q, want /admin/consolidation/sign?...", loc)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(loc, "/admin/consolidation/sign?"))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if q.Get("type") != "consolidation" {
		t.Fatalf("type = %q, want consolidation", q.Get("type"))
	}
	if q.Get("batch_id") == "" {
		t.Fatalf("batch_id missing from redirect: %q", loc)
	}
	gotOrderIDs := q["order_id"]
	if len(gotOrderIDs) != 2 {
		t.Fatalf("order_id count = %d, want 2 (redirect=%q)", len(gotOrderIDs), loc)
	}

	batchID, err := strconv.ParseInt(q.Get("batch_id"), 10, 64)
	if err != nil {
		t.Fatalf("parse batch_id: %v", err)
	}
	batch, err := loadConsolidationBatch(context.Background(), pool, batchID)
	if err != nil {
		t.Fatalf("loadConsolidationBatch() error = %v", err)
	}
	if batch.MasterWalletID != walletID || batch.DestinationAddress != testDestinationAddress {
		t.Fatalf("batch = %+v, want wallet=%d destination=%q", batch, walletID, testDestinationAddress)
	}

	assertAuditCount(t, pool, "CONSOLIDATION_BATCH_CREATED", 1)
}

func TestCreateConsolidationBatchHandler_InvalidDestinationAddress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/batches", url.Values{
		"master_wallet_id":    {strconv.FormatInt(walletID, 10)},
		"destination_address": {"not-a-valid-address"},
		"order_id":            {strconv.FormatInt(ord.ID, 10)},
		"totp_code":           {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestCreateConsolidationBatchHandler_NoOrderSelected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/batches", url.Values{
		"master_wallet_id":    {strconv.FormatInt(walletID, 10)},
		"destination_address": {testDestinationAddress},
		"totp_code":           {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestCreateFeeTopupBatchHandler_RedirectsToSignPage(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-topup-batches", url.Values{
		"master_wallet_id":   {strconv.FormatInt(walletID, 10)},
		"fee_source":         {"A1"},
		"fee_source_address": {testSourceAddress},
		"amount_per_order":   {"1"},
		"order_id":           {strconv.FormatInt(ord.ID, 10)},
		"totp_code":          {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body = %s", resp.StatusCode, readBody(t, resp))
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/admin/consolidation/sign?") {
		t.Fatalf("Location = %q, want /admin/consolidation/sign?...", loc)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(loc, "/admin/consolidation/sign?"))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if q.Get("type") != "fee-topup" {
		t.Fatalf("type = %q, want fee-topup", q.Get("type"))
	}
	if q.Get("amount_per_order") != "1000000" {
		t.Fatalf("amount_per_order = %q, want 1000000 (redirect=%q)", q.Get("amount_per_order"), loc)
	}

	batchID, err := strconv.ParseInt(q.Get("batch_id"), 10, 64)
	if err != nil {
		t.Fatalf("parse batch_id: %v", err)
	}
	batch, err := loadFeeTopupBatch(context.Background(), pool, batchID)
	if err != nil {
		t.Fatalf("loadFeeTopupBatch() error = %v", err)
	}
	if batch.MasterWalletID != walletID || batch.FeeSourceAddress != testSourceAddress {
		t.Fatalf("batch = %+v, want wallet=%d source=%q", batch, walletID, testSourceAddress)
	}

	assertAuditCount(t, pool, "FEE_TOPUP_BATCH_CREATED", 1)
}

func TestCreateFeeTopupBatchHandler_InvalidFeeSource(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-topup-batches", url.Values{
		"master_wallet_id":   {strconv.FormatInt(walletID, 10)},
		"fee_source":         {"A3"},
		"fee_source_address": {testSourceAddress},
		"amount_per_order":   {"1"},
		"order_id":           {strconv.FormatInt(ord.ID, 10)},
		"totp_code":          {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestCreateFeeTopupBatchHandler_InvalidAmount(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)

	resp := postForm(t, srv, cookie, "/admin/consolidation/fee-topup-batches", url.Values{
		"master_wallet_id":   {strconv.FormatInt(walletID, 10)},
		"fee_source":         {"A1"},
		"fee_source_address": {testSourceAddress},
		"amount_per_order":   {"0"},
		"order_id":           {strconv.FormatInt(ord.ID, 10)},
		"totp_code":          {totpCodeAt(t, secret, time.Now())},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, readBody(t, resp))
	}
}

// TestBatchWriteRoutes_RequireFreshTOTP ADR-0016: every POST requires its
// own totp_code every time (no freshness grace window) — a request with no
// code redirects to returnTo ("/admin/consolidation") with
// totp_error=missing, not to the separate /admin/reverify-totp page.
func TestBatchWriteRoutes_RequireFreshTOTP(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool}
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "order-1")
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	consolidationResp := postForm(t, srv, cookie, "/admin/consolidation/batches", url.Values{
		"master_wallet_id":    {strconv.FormatInt(walletID, 10)},
		"destination_address": {testDestinationAddress},
		"order_id":            {strconv.FormatInt(ord.ID, 10)},
	})
	consolidationLoc, err := url.Parse(consolidationResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if consolidationResp.StatusCode != http.StatusSeeOther || consolidationLoc.Path != "/admin/consolidation" || consolidationLoc.Query().Get("totp_error") != "missing" || consolidationLoc.Query().Get("master_wallet_id") != strconv.FormatInt(walletID, 10) {
		t.Fatalf("batches: status=%d location=%q, want 303 to /admin/consolidation?master_wallet_id=%d&totp_error=missing（第15項：導回該錢包待歸集頁）", consolidationResp.StatusCode, consolidationResp.Header.Get("Location"), walletID)
	}

	feeTopupResp := postForm(t, srv, cookie, "/admin/consolidation/fee-topup-batches", url.Values{
		"master_wallet_id":   {strconv.FormatInt(walletID, 10)},
		"fee_source":         {"A1"},
		"fee_source_address": {testSourceAddress},
		"amount_per_order":   {"1"},
		"order_id":           {strconv.FormatInt(ord.ID, 10)},
	})
	feeTopupLoc, err := url.Parse(feeTopupResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if feeTopupResp.StatusCode != http.StatusSeeOther || feeTopupLoc.Path != "/admin/consolidation" || feeTopupLoc.Query().Get("totp_error") != "missing" || feeTopupLoc.Query().Get("master_wallet_id") != strconv.FormatInt(walletID, 10) {
		t.Fatalf("fee-topup-batches: status=%d location=%q, want 303 to /admin/consolidation?master_wallet_id=%d&totp_error=missing（第15項：導回該錢包待歸集頁）", feeTopupResp.StatusCode, feeTopupResp.Header.Get("Location"), walletID)
	}
}
