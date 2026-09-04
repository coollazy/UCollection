package consolidation

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPendingListPage_NoWallets(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "尚未設定代收主錢包") {
		t.Fatalf("body missing no-wallets message: %s", body)
	}
}

func TestPendingListPage_SingleWalletSkipsPicker(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "single-wallet")

	mock := newMockTronGrid()
	mock.enqueue("/v1/accounts/"+o.Address, http.StatusOK, accountBalanceFixture(500))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, o.Address) {
		t.Fatalf("body missing pending order's address (picker should have been skipped): %s", body)
	}
}

func TestPendingListPage_MultipleWalletsShowsPicker(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	newMasterWallet(t, pool)
	newMasterWallet(t, pool)

	mock := newMockTronGrid() // no balance queries expected — picker page doesn't list pending items
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "master_wallet_id=1") || !strings.Contains(body, "master_wallet_id=2") {
		t.Fatalf("body missing wallet picker links: %s", body)
	}
}

func TestPendingListPage_TriggersReconcile(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reconcile-on-load")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-page-load","blockNumber":1,"receipt":{"result":"SUCCESS"}}`)
	mock.enqueue("/v1/accounts/"+o.Address, http.StatusOK, `{"data":[],"success":true,"meta":{}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	itemID := insertConsolidationItem(t, pool, batchID, o.ID, 1000, "tx-page-load", "broadcasting")

	resp := doGet(t, srv, cookie, "/admin/consolidation")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	item, err := loadConsolidationItem(context.Background(), pool, itemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "success" {
		t.Fatalf("BroadcastStatus = %q, want success — page load should have reconciled it", item.BroadcastStatus)
	}
}

func TestPendingListPage_RequireSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)

	resp := doGet(t, srv, nil, "/admin/consolidation")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login", resp.StatusCode)
	}
}
