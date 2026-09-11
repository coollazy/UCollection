package consolidation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestConsolidationPrepareAndBroadcast_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "flow-success")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(88888))
	mock.enqueue("/wallet/triggersmartcontract", http.StatusOK, `{"transaction":{"txID":"tx-success-1","raw_data_hex":"aa","visible":true},"result":{"result":true}}`)
	mock.enqueue("/wallet/broadcasttransaction", http.StatusOK, `{"result":true,"txid":"tx-success-1"}`)
	tc := mock.start(t)

	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}

	prepResp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID})
	if prepResp.StatusCode != http.StatusOK {
		t.Fatalf("prepare status = %d", prepResp.StatusCode)
	}
	prep := decodeJSON[prepareResponse](t, prepResp)
	if prep.TxID != "tx-success-1" {
		t.Fatalf("prep.TxID = %q", prep.TxID)
	}

	broadcastResp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-success-1","raw_data_hex":"aa","visible":true,"signature":["deadbeef"]}`),
	})
	if broadcastResp.StatusCode != http.StatusOK {
		t.Fatalf("broadcast status = %d", broadcastResp.StatusCode)
	}
	result := decodeJSON[broadcastResponse](t, broadcastResp)
	if result.Status != "success" {
		t.Fatalf("result.Status = %q, want success", result.Status)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want consolidated", got.ConsolidationStatus)
	}

	// Auto-saved to the address book since it wasn't already there.
	book, err := ListAddressBook(context.Background(), deps)
	if err != nil {
		t.Fatalf("ListAddressBook() error = %v", err)
	}
	if len(book) != 1 || book[0].Address != testDestinationAddress {
		t.Fatalf("address book = %+v, want auto-saved destination", book)
	}
}

func TestConsolidationPrepare_BatchOrderMismatch(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletA := newMasterWallet(t, pool)
	walletB := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletA, "mismatch")

	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletB, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a batch/order master-wallet mismatch", resp.StatusCode)
	}
}

func TestConsolidationPrepare_AlreadyConsolidatedRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "already-consolidated")
	if err := order.MarkConsolidated(context.Background(), pool, o.ID); err != nil {
		t.Fatalf("MarkConsolidated() error = %v", err)
	}

	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for an already-consolidated order", resp.StatusCode)
	}
}

func TestConsolidationPrepare_NoBalanceRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "no-balance")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(0))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for zero balance", resp.StatusCode)
	}
}

func TestConsolidationBroadcast_ItemNotPendingRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "double-broadcast")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(1000))
	mock.enqueue("/wallet/triggersmartcontract", http.StatusOK, `{"transaction":{"txID":"tx-double","raw_data_hex":"aa","visible":true},"result":{"result":true}}`)
	mock.enqueue("/wallet/broadcasttransaction", http.StatusOK, `{"result":true,"txid":"tx-double"}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	prep := decodeJSON[prepareResponse](t, postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID}))

	body := map[string]any{"item_id": prep.ItemID, "transaction": json.RawMessage(`{"txID":"tx-double","signature":["ab"]}`)}
	first := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/broadcast", body)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first broadcast status = %d", first.StatusCode)
	}

	// The second attempt is dispatched to broadcastConsolidationHandler
	// directly rather than through another postJSON+code() round trip:
	// TOTP's ±1 period (30s) skew window only has two distinct
	// non-replayed codes available within a single fast-running test (see
	// totpCodeSequence's doc comment) — a third real step-up verification
	// this soon isn't obtainable without an actual 30s+ sleep. The
	// item-not-pending rejection this asserts is broadcastConsolidationHandler's
	// own logic, unrelated to what gates it, so exercising it directly is
	// both simpler and still covers the real behavior under test.
	secondRec := httptest.NewRecorder()
	secondBody, _ := json.Marshal(body)
	secondReq := httptest.NewRequest(http.MethodPost, "/tron-proxy/consolidation/broadcast", strings.NewReader(string(secondBody)))
	broadcastConsolidationHandler(deps).ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("second broadcast (same item) status = %d, want 409 — an item must never be broadcast twice", secondRec.Code)
	}
}

func TestConsolidationBroadcast_TxIDMismatchRejected(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "txid-mismatch")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(1000))
	mock.enqueue("/wallet/triggersmartcontract", http.StatusOK, `{"transaction":{"txID":"tx-real","raw_data_hex":"aa","visible":true},"result":{"result":true}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	prep := decodeJSON[prepareResponse](t, postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID}))

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-DIFFERENT-from-prepared","signature":["ab"]}`),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a txID that doesn't match the prepared item", resp.StatusCode)
	}

	// The item must still be 'pending' — the mismatched broadcast must never
	// have reached TronGrid (no /wallet/broadcasttransaction was queued;
	// mockTronGrid would have errored the test if it received an
	// unexpected request).
	item, err := loadConsolidationItem(context.Background(), pool, prep.ItemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "pending" {
		t.Fatalf("BroadcastStatus = %q, want still pending", item.BroadcastStatus)
	}
}

func TestConsolidationBroadcast_ExplicitRejectionMarksFailed(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "explicit-reject")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(1000))
	mock.enqueue("/wallet/triggersmartcontract", http.StatusOK, `{"transaction":{"txID":"tx-rejected","raw_data_hex":"aa","visible":true},"result":{"result":true}}`)
	mock.enqueue("/wallet/broadcasttransaction", http.StatusOK, `{"code":"SIGERROR","message":"bad signature","txid":"tx-rejected"}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	prep := decodeJSON[prepareResponse](t, postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID}))

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-rejected","signature":["ab"]}`),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	result := decodeJSON[broadcastResponse](t, resp)
	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed", result.Status)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationNotConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want still not_consolidated after an explicit broadcast rejection", got.ConsolidationStatus)
	}
}

func TestConsolidationBroadcast_NetworkFailureLeavesBroadcasting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "network-failure")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(1000))
	mock.enqueue("/wallet/triggersmartcontract", http.StatusOK, `{"transaction":{"txID":"tx-network-fail","raw_data_hex":"aa","visible":true},"result":{"result":true}}`)
	// Deliberately do NOT enqueue a /wallet/broadcasttransaction response —
	// mockTronGrid returns 500 with no body for unqueued paths, which
	// BroadcastTransaction still parses as an HTTP response (not a true
	// network failure) and correctly reports as a definite failure. To
	// simulate a genuine network-layer failure we need a connection that
	// dies mid-request; see the dedicated hijack-based test in
	// internal/tronclient for that scenario — this test instead confirms
	// the item is NOT silently marked "success" or double-processed when
	// TronGrid's broadcast response can't be parsed as legitimate JSON.
	mock.enqueue("/wallet/broadcasttransaction", http.StatusOK, `not valid json`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	prep := decodeJSON[prepareResponse](t, postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID}))

	resp := postJSON(t, srv, cookie, code(), "/tron-proxy/consolidation/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-network-fail","signature":["ab"]}`),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	result := decodeJSON[broadcastResponse](t, resp)
	// An unparseable-but-received response is a definite failure (TronGrid
	// DID respond), not "broadcasting" — see decideBroadcastOutcome's
	// doc comment for why these are different outcomes.
	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed for an unparseable-but-received response", result.Status)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationNotConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want still not_consolidated", got.ConsolidationStatus)
	}
}

// TestConsolidationBroadcast_TrueNetworkFailureLeavesBroadcasting exercises
// a genuine network-layer failure (connection dies mid-request, no HTTP
// response at all — see internal/tronclient's identical technique) rather
// than a received-but-unparseable response. This is the specific scenario
// CLAUDE.md 安全鐵律7/驗證結論-06 觀察4 are about: the item must be left in
// 'broadcasting' for later reconciliation, never marked 'failed' (which
// would invite an unsafe re-broadcast of the same funds).
func TestConsolidationBroadcast_TrueNetworkFailureLeavesBroadcasting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "true-network-failure")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wallet/triggerconstantcontract":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(constantContractBalanceFixture(1000)))
		case "/wallet/triggersmartcontract":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"transaction":{"txID":"tx-true-network-fail","raw_data_hex":"aa","visible":true},"result":{"result":true}}`))
		case "/wallet/broadcasttransaction":
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), USDTContractAddress: testUSDTContract}
	testSrv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	prep := decodeJSON[prepareResponse](t, postJSON(t, testSrv, cookie, code(), "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID}))

	resp := postJSON(t, testSrv, cookie, code(), "/tron-proxy/consolidation/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-true-network-fail","signature":["ab"]}`),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	result := decodeJSON[broadcastResponse](t, resp)
	if result.Status != "broadcasting" {
		t.Fatalf("Status = %q, want broadcasting (unknown — must be left for reconcile, never marked failed)", result.Status)
	}

	item, err := loadConsolidationItem(context.Background(), pool, prep.ItemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "broadcasting" {
		t.Fatalf("BroadcastStatus = %q, want broadcasting", item.BroadcastStatus)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationNotConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want still not_consolidated while broadcast outcome is unknown", got.ConsolidationStatus)
	}
}

func TestFeeTopupPrepareAndBroadcast_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "fee-topup-success")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/createtransaction", http.StatusOK, `{"txID":"tx-fee-1","raw_data_hex":"bb","visible":true}`)
	mock.enqueue("/wallet/broadcasttransaction", http.StatusOK, `{"result":true,"txid":"tx-fee-1"}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie, secret := newActiveSessionWithTOTP(t, pool)
	code := totpCodeSequence(t, secret)

	batchID, err := CreateFeeTopupBatch(context.Background(), deps, walletID, "A1", testSourceAddress)
	if err != nil {
		t.Fatalf("CreateFeeTopupBatch() error = %v", err)
	}

	prepResp := postJSON(t, srv, cookie, code(), "/tron-proxy/fee-topup/prepare", map[string]any{"batch_id": batchID, "order_id": o.ID, "amount": 5_000000})
	if prepResp.StatusCode != http.StatusOK {
		t.Fatalf("prepare status = %d", prepResp.StatusCode)
	}
	prep := decodeJSON[prepareResponse](t, prepResp)

	broadcastResp := postJSON(t, srv, cookie, code(), "/tron-proxy/fee-topup/broadcast", map[string]any{
		"item_id":     prep.ItemID,
		"transaction": json.RawMessage(`{"txID":"tx-fee-1","signature":["ab"]}`),
	})
	if broadcastResp.StatusCode != http.StatusOK {
		t.Fatalf("broadcast status = %d", broadcastResp.StatusCode)
	}
	result := decodeJSON[broadcastResponse](t, broadcastResp)
	if result.Status != "success" {
		t.Fatalf("Status = %q, want success", result.Status)
	}

	// Fee top-up success must NOT mark the order consolidated — it's only a
	// prerequisite step, not the consolidation itself.
	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationNotConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want unaffected by fee top-up", got.ConsolidationStatus)
	}

	// 廣播成功後，手動指定的 TRX 來源地址應自動存入手續費來源地址簿
	// （需求書5.9 v0.39，best-effort，比照歸集地址簿的 AutoSaveIfNew）。
	fsb, err := ListFeeSourceBook(context.Background(), deps)
	if err != nil {
		t.Fatalf("ListFeeSourceBook() error = %v", err)
	}
	if len(fsb) != 1 || fsb[0].Address != testSourceAddress {
		t.Fatalf("fee source book = %+v, want auto-saved %s", fsb, testSourceAddress)
	}
}

func TestConsolidationRoutes_RequireSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)

	resp := postJSON(t, srv, nil, "", "/tron-proxy/consolidation/prepare", map[string]any{"batch_id": 1, "order_id": 1})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login when no session cookie is present", resp.StatusCode)
	}
}
