package order

import (
	"context"
	"fmt"
	"testing"
)

func TestListOrders_FiltersAndPaginates(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)

	var ids []int64
	for i := 1; i <= 3; i++ {
		o, err := CreateOrder(ctx, pool, defaultParams(fmt.Sprintf("list-%d", i), walletID))
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		ids = append(ids, o.ID)
	}
	forceStatus(t, pool, ids[0], StatusCompleted)
	forceStatus(t, pool, ids[1], StatusExpired)

	t.Run("no filter returns all, newest first", func(t *testing.T) {
		got, total, err := ListOrders(ctx, pool, ListFilter{Limit: 50})
		if err != nil {
			t.Fatalf("ListOrders() error = %v", err)
		}
		if total != 3 || len(got) != 3 {
			t.Fatalf("total=%d len=%d, want 3/3", total, len(got))
		}
		if got[0].ID != ids[2] {
			t.Errorf("first row ID = %d, want %d (newest first)", got[0].ID, ids[2])
		}
	})

	t.Run("status filter", func(t *testing.T) {
		got, total, err := ListOrders(ctx, pool, ListFilter{Statuses: []Status{StatusCompleted}, Limit: 50})
		if err != nil {
			t.Fatalf("ListOrders() error = %v", err)
		}
		if total != 1 || len(got) != 1 || got[0].ID != ids[0] {
			t.Fatalf("got total=%d rows=%v, want exactly order %d", total, got, ids[0])
		}
	})

	t.Run("merchant_order_no ILIKE", func(t *testing.T) {
		got, total, err := ListOrders(ctx, pool, ListFilter{MerchantOrderNo: "list-1", Limit: 50})
		if err != nil {
			t.Fatalf("ListOrders() error = %v", err)
		}
		if total != 1 || len(got) != 1 {
			t.Fatalf("total=%d len=%d, want 1/1", total, len(got))
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page1, total, err := ListOrders(ctx, pool, ListFilter{Limit: 2, Offset: 0})
		if err != nil {
			t.Fatalf("ListOrders() error = %v", err)
		}
		if total != 3 || len(page1) != 2 {
			t.Fatalf("page1 total=%d len=%d, want 3/2", total, len(page1))
		}
		page2, total, err := ListOrders(ctx, pool, ListFilter{Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("ListOrders() error = %v", err)
		}
		if total != 3 || len(page2) != 1 {
			t.Fatalf("page2 total=%d len=%d, want 3/1", total, len(page2))
		}
	})
}

func TestListIncomingTransactions(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o, err := CreateOrder(ctx, pool, defaultParams("incoming-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, 'tx-a', 0, 50000000, true, 1000), ($1, 'tx-b', 1, 60000000, false, 1001)
	`, o.ID)
	if err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}

	got, err := ListIncomingTransactions(ctx, pool, o.ID)
	if err != nil {
		t.Fatalf("ListIncomingTransactions() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].TxHash != "tx-a" || !got[0].Confirmed {
		t.Errorf("got[0] = %+v, want tx-a confirmed=true", got[0])
	}
	if got[1].TxHash != "tx-b" || got[1].Confirmed {
		t.Errorf("got[1] = %+v, want tx-b confirmed=false", got[1])
	}
}
