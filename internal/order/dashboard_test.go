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

	// COMPLETED with a system-flagged late finality confirmation (階段06情境E)
	// must count as abnormal — this is the case dashboard visibility was
	// missing entirely before this change.
	completedLateConfirm, err := CreateOrder(ctx, pool, defaultParams("dash-completed-late-confirm", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, completedLateConfirm.ID, StatusCompleted)
	if _, err := pool.Exec(ctx, `
		INSERT INTO audit_logs (actor, action_type, target_type, target_id, detail)
		VALUES ('system', 'ILLEGAL_STATE_TRANSITION', 'order', $1, '{}')
	`, completedLateConfirm.ID); err != nil {
		t.Fatalf("insert audit_logs: %v", err)
	}

	// COMPLETED with an ILLEGAL_STATE_TRANSITION audit entry from a
	// *rejected admin override attempt* (internal/admin/orders_override.go
	// writes the same action_type for an unrelated reason: someone tried an
	// invalid manual transition and got denied) must NOT count as
	// abnormal — this order's amount is fine, only actor="system" entries
	// signal a real amount discrepancy.
	completedAdminRejected, err := CreateOrder(ctx, pool, defaultParams("dash-completed-admin-rejected", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, completedAdminRejected.ID, StatusCompleted)
	if _, err := pool.Exec(ctx, `
		INSERT INTO audit_logs (actor, action_type, target_type, target_id, detail)
		VALUES ('admin', 'ILLEGAL_STATE_TRANSITION', 'order', $1, '{}')
	`, completedAdminRejected.ID); err != nil {
		t.Fatalf("insert audit_logs: %v", err)
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
	// abnormal = overpaid + expiredPartial + completedLateConfirm
	// (NOT expiredEmpty, NOT completedAdminRejected)
	if got.AbnormalCount != 3 {
		t.Errorf("AbnormalCount = %d, want 3", got.AbnormalCount)
	}
}
