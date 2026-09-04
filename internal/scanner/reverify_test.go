package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// TestReverifyOrder_MergesAndConfirmsWithoutEvaluating covers 技術架構設計第11節
// 「手動重新檢查此地址」's two defining differences from 任務2/3: it must still
// merge/confirm incoming_transactions via the exact same dedup key, but must
// NOT trigger any automatic state transition (order stays PENDING even
// though the merged amount would otherwise qualify it for CONFIRMING).
func TestReverifyOrder_MergesAndConfirmsWithoutEvaluating(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "reverify-1", walletID, 100)

	toHex := toHexAddress(t, o.Address)
	eventTs := time.Now().UnixMilli() - 10_000
	// Both the only_confirmed=false and only_confirmed=true passes hit this
	// same static mock server and get the same canned event back — the
	// first pass merges it (confirmed=false), the second marks it
	// confirmed=true, exactly like 任務2 then 任務3 would across two polls.
	srv := mockTronGridOnce(t, eventsResponseJSON([]mockEvent{
		{TxID: "tx-reverify", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: eventTs, FromHex: arbitrarySenderHex, ToHex: toHex, Value: "100"},
	}, ""))

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := ReverifyOrder(ctx, deps, o); err != nil {
		t.Fatalf("ReverifyOrder() error = %v", err)
	}

	if n := countIncomingTx(t, pool, o.ID); n != 1 {
		t.Fatalf("incoming_transactions count = %d, want 1", n)
	}
	var confirmed bool
	if err := pool.QueryRow(ctx, `SELECT confirmed FROM incoming_transactions WHERE order_id = $1`, o.ID).Scan(&confirmed); err != nil {
		t.Fatalf("load confirmed: %v", err)
	}
	if !confirmed {
		t.Error("confirmed = false, want true (only_confirmed=true pass should have marked it)")
	}

	// The whole point: target=100/tolerance=0 means this amount would
	// normally push the order straight to CONFIRMING (see the equivalent
	// scanEventsOnce test) — reverify must leave it exactly where it was.
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusPending {
		t.Fatalf("order status = %s, want PENDING (reverify must not evaluate transitions)", got)
	}

	// scan_checkpoint must be untouched — reverify is address-scoped, not a
	// checkpoint-driven sweep. resetDB truncates it with no reseed, so "no
	// row" is itself proof nothing wrote to it.
	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scan_checkpoint`).Scan(&rowCount); err != nil {
		t.Fatalf("count checkpoint rows: %v", err)
	}
	if rowCount != 0 {
		t.Errorf("scan_checkpoint row count = %d, want 0 (reverify must not touch it)", rowCount)
	}
}

// TestReverifyOrder_IgnoresOtherAddresses confirms the application-layer
// ev.To filter (技術架構設計第11節：「此查詢仍是對USDT合約全域事件流查詢，to過濾在應用
// 層做」) — an event for a different order's address must not leak in.
func TestReverifyOrder_IgnoresOtherAddresses(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	target := testOrder(t, ctx, pool, "reverify-target", walletID, 100)
	other := testOrder(t, ctx, pool, "reverify-other", walletID, 100)

	otherHex := toHexAddress(t, other.Address)
	eventTs := time.Now().UnixMilli() - 10_000
	srv := mockTronGridOnce(t, eventsResponseJSON([]mockEvent{
		{TxID: "tx-other", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: eventTs, FromHex: arbitrarySenderHex, ToHex: otherHex, Value: "100"},
	}, ""))

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := ReverifyOrder(ctx, deps, target); err != nil {
		t.Fatalf("ReverifyOrder() error = %v", err)
	}

	if n := countIncomingTx(t, pool, target.ID); n != 0 {
		t.Fatalf("target order incoming_transactions count = %d, want 0", n)
	}
	if n := countIncomingTx(t, pool, other.ID); n != 0 {
		t.Fatalf("other order incoming_transactions count = %d, want 0 (reverify was scoped to target's address only)", n)
	}
}
