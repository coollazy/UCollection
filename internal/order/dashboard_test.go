package order

import (
	"context"
	"testing"
)

func TestGetDashboardStats(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)

	completed, err := CreateOrder(ctx, pool, defaultParams("dash-completed", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, completed.ID, StatusCompleted)
	if _, err := pool.Exec(ctx, `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, 'tx-completed', 0, 100000000, true, 1000)
	`, completed.ID); err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}

	if _, err := CreateOrder(ctx, pool, defaultParams("dash-pending", walletID)); err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	overpaid, err := CreateOrder(ctx, pool, defaultParams("dash-overpaid", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, overpaid.ID, StatusOverpaid)

	// EXPIRED with zero incoming_transactions (the common "never paid" case)
	// must NOT count as abnormal.
	expiredEmpty, err := CreateOrder(ctx, pool, defaultParams("dash-expired-empty", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, expiredEmpty.ID, StatusExpired)

	// EXPIRED with a partial payment must count as abnormal (需求書5.2的
	// 「收到部分金額」情況).
	expiredPartial, err := CreateOrder(ctx, pool, defaultParams("dash-expired-partial", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, expiredPartial.ID, StatusExpired)
	if _, err := pool.Exec(ctx, `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, 'tx-partial', 0, 1000000, false, 1000)
	`, expiredPartial.ID); err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}

	got, err := GetDashboardStats(ctx, pool)
	if err != nil {
		t.Fatalf("GetDashboardStats() error = %v", err)
	}
	if got.TotalCollected != 100000000 {
		t.Errorf("TotalCollected = %d, want 100000000 (only confirmed COMPLETED/OVERPAID amounts)", got.TotalCollected)
	}
	if got.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", got.PendingCount)
	}
	// abnormal = overpaid + expiredPartial (NOT expiredEmpty)
	if got.AbnormalCount != 2 {
		t.Errorf("AbnormalCount = %d, want 2", got.AbnormalCount)
	}
}
