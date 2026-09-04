package consolidation

import (
	"context"
	"testing"
)

func TestItemsForOrder(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "items-for-order-1")

	var batchID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO consolidation_batches (master_wallet_id, destination_address) VALUES ($1, $2) RETURNING id
	`, walletID, testDestinationAddress).Scan(&batchID)
	if err != nil {
		t.Fatalf("insert consolidation_batches: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO consolidation_items (batch_id, order_id, amount, tx_hash, broadcast_status, error_detail)
		VALUES ($1, $2, 12345, 'tx-consolidation', 'success', NULL)
	`, batchID, o.ID); err != nil {
		t.Fatalf("insert consolidation_items: %v", err)
	}

	var feeBatchID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO fee_topup_batches (master_wallet_id, fee_source, fee_source_address) VALUES ($1, 'A1', $2) RETURNING id
	`, walletID, testSourceAddress).Scan(&feeBatchID)
	if err != nil {
		t.Fatalf("insert fee_topup_batches: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO fee_topup_items (batch_id, order_id, amount, tx_hash, broadcast_status, error_detail)
		VALUES ($1, $2, 5000000, 'tx-feetopup', 'failed', 'broadcast rejected')
	`, feeBatchID, o.ID); err != nil {
		t.Fatalf("insert fee_topup_items: %v", err)
	}

	items, feeTopups, err := ItemsForOrder(ctx, pool, o.ID)
	if err != nil {
		t.Fatalf("ItemsForOrder() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].BatchID != batchID || items[0].Amount != 12345 || items[0].TxHash != "tx-consolidation" || items[0].BroadcastStatus != "success" || items[0].ErrorDetail != nil {
		t.Errorf("items[0] = %+v, unexpected", items[0])
	}
	if len(feeTopups) != 1 {
		t.Fatalf("len(feeTopups) = %d, want 1", len(feeTopups))
	}
	if feeTopups[0].BatchID != feeBatchID || feeTopups[0].BroadcastStatus != "failed" || feeTopups[0].ErrorDetail == nil || *feeTopups[0].ErrorDetail != "broadcast rejected" {
		t.Errorf("feeTopups[0] = %+v, unexpected", feeTopups[0])
	}

	// A different order must see neither.
	other := newOrder(t, pool, walletID, "items-for-order-2")
	items, feeTopups, err = ItemsForOrder(ctx, pool, other.ID)
	if err != nil {
		t.Fatalf("ItemsForOrder() error = %v", err)
	}
	if len(items) != 0 || len(feeTopups) != 0 {
		t.Errorf("other order should have no items, got items=%v feeTopups=%v", items, feeTopups)
	}
}
