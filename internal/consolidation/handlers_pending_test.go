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
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(500))
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
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(0))
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

// TestPendingListPage_RendersAddressBookSelector 覆蓋第9/10項：待歸集頁的目的地
// 地址欄位串接歸集地址簿、手續費來源地址欄位串接手續費來源地址簿——兩者各自渲染出
// 下拉選單（含地址簿項目與「手動輸入新地址」選項）及共用的切換 JS。
func TestPendingListPage_RendersAddressBookSelector(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "addrbook-selector")

	if _, err := CreateAddressBookEntry(context.Background(), Deps{Pool: pool}, testDestinationAddress, "測試冷錢包"); err != nil {
		t.Fatalf("CreateAddressBookEntry() error = %v", err)
	}
	if _, err := CreateFeeSourceBookEntry(context.Background(), Deps{Pool: pool}, testSourceAddress, "測試手續費錢包"); err != nil {
		t.Fatalf("CreateFeeSourceBookEntry() error = %v", err)
	}

	mock := newMockTronGrid()
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(500))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := doGet(t, srv, cookie, "/admin/consolidation")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)

	// 待歸集項目必須有被列出，否則模板走空清單分支、根本不渲染歸集表單/選單。
	if !strings.Contains(body, o.Address) {
		t.Fatalf("expected pending entry to be listed: %s", body)
	}
	for _, want := range []string{
		`data-target-input="dest-addr"`,          // 目的地地址下拉（第9項）
		"測試冷錢包",                                  // 目的地地址簿備註 label
		`data-target-input="fee-source-address"`, // 手續費來源地址下拉（第10項）
		testSourceAddress,                        // 來源地址簿項目出現在 option value
		"測試手續費錢包",                                // 來源地址簿備註 label
		testDestinationAddress,                   // 目的地地址簿項目出現在 option value
		"__manual__",                             // 兩個下拉都保留手動輸入新地址的選項
		"/static/js/address-book-select.js",      // 顯示/隱藏切換的 JS（兩個下拉共用）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}
