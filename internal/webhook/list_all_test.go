package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/order"
)

func TestListAll_FiltersAndPagination(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	// Two orders under the SAME wallet (not two calls to testOrderID, which
	// each insert their own master_wallets row sharing the same testXpub —
	// that collides on orders.address since both would derive index 1 from
	// an identical xpub, see internal/consolidation's sign_test.go for the
	// same footgun documented previously).
	order1ID := testOrderID(t, pool)
	var walletID int64
	if err := pool.QueryRow(ctx, `SELECT master_wallet_id FROM orders WHERE id = $1`, order1ID).Scan(&walletID); err != nil {
		t.Fatalf("query wallet id: %v", err)
	}
	o2, err := order.CreateOrder(ctx, pool, order.CreateParams{
		MerchantOrderNo:                 randomHex(t),
		MasterWalletID:                  walletID,
		TargetAmount:                    100,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          0,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	order1 := order1ID
	order2 := o2.ID

	d1 := insertDelivery(t, pool, order1, "pending", nil)
	d2 := insertDelivery(t, pool, order2, "delivered", nil)
	if _, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET event_type = 'ORDER_EXPIRED' WHERE id = $1`, d1); err != nil {
		t.Fatalf("set event_type: %v", err)
	}

	// Unfiltered: both deliveries, newest first (d2 has the higher id).
	all, total, err := ListAll(ctx, pool, ListFilter{})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(all))
	}
	if all[0].ID != d2 {
		t.Errorf("all[0].ID = %d, want %d (newest first)", all[0].ID, d2)
	}

	// Filter by status.
	filtered, total, err := ListAll(ctx, pool, ListFilter{Status: "delivered"})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].ID != d2 {
		t.Fatalf("status filter: total=%d len=%d, want 1/1 matching %d", total, len(filtered), d2)
	}

	// Filter by order_id.
	filtered, total, err = ListAll(ctx, pool, ListFilter{OrderID: &order1})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].ID != d1 {
		t.Fatalf("order_id filter: total=%d len=%d, want 1/1 matching %d", total, len(filtered), d1)
	}

	// Filter by event_type.
	filtered, total, err = ListAll(ctx, pool, ListFilter{EventType: "ORDER_COMPLETED"})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].ID != d2 {
		t.Fatalf("event_type filter: total=%d len=%d, want 1/1 matching %d", total, len(filtered), d2)
	}

	// Pagination.
	page, total, err := ListAll(ctx, pool, ListFilter{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if total != 2 || len(page) != 1 {
		t.Fatalf("paginated total=%d len=%d, want 2/1", total, len(page))
	}
}

func TestListAll_IncludesAttempts(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	orderID := testOrderID(t, pool)
	deliveryID := insertDelivery(t, pool, orderID, "failed", nil)
	insertOldAttempt(t, pool, deliveryID, time.Now())

	all, _, err := ListAll(ctx, pool, ListFilter{})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(all) != 1 || len(all[0].Attempts) != 1 {
		t.Fatalf("expected 1 delivery with 1 attempt, got %+v", all)
	}
}
