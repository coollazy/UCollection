package order

import (
	"context"
	"fmt"
	"testing"
)

func TestSumConfirmedAmountsForOrders(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)

	o1, err := CreateOrder(ctx, pool, defaultParams("agg-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	o2, err := CreateOrder(ctx, pool, defaultParams("agg-2", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	o3, err := CreateOrder(ctx, pool, defaultParams("agg-3", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	var txSeq int
	insertIncoming := func(orderID int64, amount int64, confirmed bool) {
		txSeq++
		_, err := pool.Exec(ctx, `
			INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
			VALUES ($1, $2, 0, $3, $4, 1)
		`, orderID, fmt.Sprintf("tx-%d", txSeq), amount, confirmed)
		if err != nil {
			t.Fatalf("insert incoming_transactions: %v", err)
		}
	}
	insertIncoming(o1.ID, 30_000000, true)
	insertIncoming(o1.ID, 20_000000, true)
	insertIncoming(o1.ID, 5_000000, false) // unconfirmed, must not count
	insertIncoming(o2.ID, 100_000000, true)
	// o3 has no incoming_transactions rows at all.

	sums, err := SumConfirmedAmountsForOrders(ctx, pool, []int64{o1.ID, o2.ID, o3.ID})
	if err != nil {
		t.Fatalf("SumConfirmedAmountsForOrders() error = %v", err)
	}
	if sums[o1.ID] != 50_000000 {
		t.Errorf("sums[o1] = %d, want 50_000000", sums[o1.ID])
	}
	if sums[o2.ID] != 100_000000 {
		t.Errorf("sums[o2] = %d, want 100_000000", sums[o2.ID])
	}
	if _, ok := sums[o3.ID]; ok {
		t.Errorf("sums[o3] present = %v, want absent (no rows)", sums[o3.ID])
	}
}

func TestSumConfirmedAmountsForOrders_EmptyInput(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	sums, err := SumConfirmedAmountsForOrders(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("SumConfirmedAmountsForOrders() error = %v", err)
	}
	if len(sums) != 0 {
		t.Errorf("sums = %v, want empty", sums)
	}
}

func TestLatestTransitionTimes(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)

	o1, err := CreateOrder(ctx, pool, defaultParams("trans-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	o2, err := CreateOrder(ctx, pool, defaultParams("trans-2", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE orders SET status = 'CONFIRMATION_STALLED' WHERE id = $1`, o1.ID); err != nil {
		t.Fatalf("force STALLED: %v", err)
	}
	if err := ManualTransition(ctx, pool, o1.ID, StatusCompleted, "admin", "test"); err != nil {
		t.Fatalf("ManualTransition() error = %v", err)
	}

	times, err := LatestTransitionTimes(ctx, pool, []int64{o1.ID, o2.ID})
	if err != nil {
		t.Fatalf("LatestTransitionTimes() error = %v", err)
	}
	if _, ok := times[o1.ID]; !ok {
		t.Error("times[o1] missing, want a transition timestamp")
	}
	if _, ok := times[o2.ID]; ok {
		t.Errorf("times[o2] present = %v, want absent (no transitions yet)", times[o2.ID])
	}
}

func TestIsTerminalStatus(t *testing.T) {
	cases := map[Status]bool{
		StatusPending:             false,
		StatusConfirming:          false,
		StatusCompleted:           true,
		StatusOverpaid:            true,
		StatusConfirmationStalled: true,
		StatusExpired:             true,
	}
	for status, want := range cases {
		if got := IsTerminalStatus(status); got != want {
			t.Errorf("IsTerminalStatus(%s) = %v, want %v", status, got, want)
		}
	}
}
