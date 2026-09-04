package consolidation

import (
	"context"
	"net/http"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

func TestReconcileBroadcasting_ConsolidationSuccess(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reconcile-success")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-reconcile-1","blockNumber":123,"receipt":{"result":"SUCCESS"}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	itemID := insertConsolidationItem(t, pool, batchID, o.ID, 1000, "tx-reconcile-1", "broadcasting")

	if err := ReconcileBroadcasting(context.Background(), deps, &walletID); err != nil {
		t.Fatalf("ReconcileBroadcasting() error = %v", err)
	}

	item, err := loadConsolidationItem(context.Background(), pool, itemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "success" {
		t.Fatalf("BroadcastStatus = %q, want success", item.BroadcastStatus)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want consolidated", got.ConsolidationStatus)
	}
}

func TestReconcileBroadcasting_ConsolidationRevertMarksFailed(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reconcile-revert")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-reconcile-2","blockNumber":123,"receipt":{"result":"REVERT"}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	itemID := insertConsolidationItem(t, pool, batchID, o.ID, 1000, "tx-reconcile-2", "broadcasting")

	if err := ReconcileBroadcasting(context.Background(), deps, &walletID); err != nil {
		t.Fatalf("ReconcileBroadcasting() error = %v", err)
	}

	item, err := loadConsolidationItem(context.Background(), pool, itemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "failed" {
		t.Fatalf("BroadcastStatus = %q, want failed", item.BroadcastStatus)
	}

	got, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != order.ConsolidationNotConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want still not_consolidated after REVERT", got.ConsolidationStatus)
	}
}

func TestReconcileBroadcasting_NotFoundYetLeavesBroadcasting(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reconcile-not-found")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}

	batchID, err := CreateConsolidationBatch(context.Background(), deps, walletID, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	itemID := insertConsolidationItem(t, pool, batchID, o.ID, 1000, "tx-reconcile-pending", "broadcasting")

	if err := ReconcileBroadcasting(context.Background(), deps, &walletID); err != nil {
		t.Fatalf("ReconcileBroadcasting() error = %v", err)
	}

	item, err := loadConsolidationItem(context.Background(), pool, itemID)
	if err != nil {
		t.Fatalf("loadConsolidationItem() error = %v", err)
	}
	if item.BroadcastStatus != "broadcasting" {
		t.Fatalf("BroadcastStatus = %q, want still broadcasting when the transaction isn't found on chain yet", item.BroadcastStatus)
	}
}

func TestReconcileBroadcasting_FeeTopupSuccessHasNoReceiptResult(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reconcile-fee-topup")

	mock := newMockTronGrid()
	// TransferContract: no receipt.result field at all (驗證結論-06附加測試).
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-fee-reconcile","blockNumber":456,"receipt":{"net_fee":268000}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}

	batchID, err := CreateFeeTopupBatch(context.Background(), deps, walletID, "A1", testSourceAddress)
	if err != nil {
		t.Fatalf("CreateFeeTopupBatch() error = %v", err)
	}
	itemID := insertFeeTopupItem(t, pool, batchID, o.ID, 5_000000, "tx-fee-reconcile", "broadcasting")

	if err := ReconcileBroadcasting(context.Background(), deps, &walletID); err != nil {
		t.Fatalf("ReconcileBroadcasting() error = %v", err)
	}

	item, err := loadFeeTopupItem(context.Background(), pool, itemID)
	if err != nil {
		t.Fatalf("loadFeeTopupItem() error = %v", err)
	}
	if item.BroadcastStatus != "success" {
		t.Fatalf("BroadcastStatus = %q, want success (blockNumber present is sufficient for TransferContract)", item.BroadcastStatus)
	}
}

func TestReconcileBroadcasting_ScopesToMasterWallet(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletA := newMasterWallet(t, pool)
	walletB := newMasterWallet(t, pool)
	// Same xpub as walletA (see newMasterWallet) — bump last_derived_index so
	// walletB's order doesn't derive the same address as walletA's.
	if _, err := pool.Exec(context.Background(), `UPDATE master_wallets SET last_derived_index = 50 WHERE id = $1`, walletB); err != nil {
		t.Fatalf("bump last_derived_index: %v", err)
	}
	oA := newOrder(t, pool, walletA, "wallet-a-order")
	oB := newOrder(t, pool, walletB, "wallet-b-order")

	mock := newMockTronGrid()
	// Only wallet A's item should be queried when scoped to walletA.
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-wallet-a","blockNumber":1,"receipt":{"result":"SUCCESS"}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}

	batchA, err := CreateConsolidationBatch(context.Background(), deps, walletA, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	batchB, err := CreateConsolidationBatch(context.Background(), deps, walletB, testDestinationAddress)
	if err != nil {
		t.Fatalf("CreateConsolidationBatch() error = %v", err)
	}
	itemA := insertConsolidationItem(t, pool, batchA, oA.ID, 1000, "tx-wallet-a", "broadcasting")
	itemB := insertConsolidationItem(t, pool, batchB, oB.ID, 1000, "tx-wallet-b", "broadcasting")

	if err := ReconcileBroadcasting(context.Background(), deps, &walletA); err != nil {
		t.Fatalf("ReconcileBroadcasting() error = %v", err)
	}

	gotA, err := loadConsolidationItem(context.Background(), pool, itemA)
	if err != nil {
		t.Fatalf("loadConsolidationItem(A) error = %v", err)
	}
	if gotA.BroadcastStatus != "success" {
		t.Fatalf("wallet A item status = %q, want success", gotA.BroadcastStatus)
	}

	gotB, err := loadConsolidationItem(context.Background(), pool, itemB)
	if err != nil {
		t.Fatalf("loadConsolidationItem(B) error = %v", err)
	}
	if gotB.BroadcastStatus != "broadcasting" {
		t.Fatalf("wallet B item status = %q, want still broadcasting — reconcile must not touch other wallets when scoped", gotB.BroadcastStatus)
	}
}

func insertConsolidationItem(t *testing.T, pool *store.Pool, batchID, orderID, amount int64, txHash, status string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO consolidation_items (batch_id, order_id, amount, tx_hash, broadcast_status)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, batchID, orderID, amount, txHash, status).Scan(&id)
	if err != nil {
		t.Fatalf("insert consolidation_items: %v", err)
	}
	return id
}

func insertFeeTopupItem(t *testing.T, pool *store.Pool, batchID, orderID, amount int64, txHash, status string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO fee_topup_items (batch_id, order_id, amount, tx_hash, broadcast_status)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, batchID, orderID, amount, txHash, status).Scan(&id)
	if err != nil {
		t.Fatalf("insert fee_topup_items: %v", err)
	}
	return id
}
