package consolidation

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

// constantContractBalanceFixture mirrors a triggerconstantcontract response
// to a balanceOf(address) call (see internal/tronclient/balance.go) —
// TRC20Balance now queries the contract directly instead of GET
// /v1/accounts/{address}, so mocks return this shape regardless of address.
func constantContractBalanceFixture(usdtBalance int64) string {
	return fmt.Sprintf(`{"result":{"result":true},"constant_result":["%064x"]}`, usdtBalance)
}

// forceOrderStatus directly overwrites orders.status — this package doesn't
// own the order state machine (internal/order does, and its own transition
// rules deliberately don't allow jumping straight to COMPLETED), so tests
// that just need a COMPLETED order to exercise the OverTarget flag bypass
// it with a raw UPDATE, same as internal/order's own tests do internally.
func forceOrderStatus(t *testing.T, pool *store.Pool, orderID int64, status order.Status) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status = $2 WHERE id = $1`, orderID, status); err != nil {
		t.Fatalf("forceOrderStatus: %v", err)
	}
}

func TestListPendingConsolidation_FiltersZeroBalance(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o1 := newOrder(t, pool, walletID, "pending-1")
	newOrder(t, pool, walletID, "pending-2")

	mock := newMockTronGrid()
	// Order of ListPendingConsolidation's candidates is by id ascending
	// (order.ListPendingConsolidation), so o1 then o2.
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(500))
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(0))
	tc := mock.start(t)

	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	got, err := ListPendingConsolidation(context.Background(), deps, walletID)
	if err != nil {
		t.Fatalf("ListPendingConsolidation() error = %v", err)
	}
	if len(got) != 1 || got[0].OrderID != o1.ID || got[0].Balance != 500 {
		t.Fatalf("got = %+v, want only order %d with balance 500", got, o1.ID)
	}
}

func TestListPendingConsolidation_OverTargetFlag(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "over-target")
	forceOrderStatus(t, pool, o.ID, order.StatusCompleted)

	mock := newMockTronGrid()
	// o.TargetAmount is 100_000000 (see newOrder); use a balance above it.
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, constantContractBalanceFixture(150_000000))
	tc := mock.start(t)

	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	got, err := ListPendingConsolidation(context.Background(), deps, walletID)
	if err != nil {
		t.Fatalf("ListPendingConsolidation() error = %v", err)
	}
	if len(got) != 1 || !got[0].OverTarget {
		t.Fatalf("got = %+v, want OverTarget=true", got)
	}
}

func TestListPendingConsolidation_ExcludesAlreadyConsolidated(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "already-done")
	if err := order.MarkConsolidated(context.Background(), pool, o.ID); err != nil {
		t.Fatalf("MarkConsolidated() error = %v", err)
	}

	mock := newMockTronGrid() // no balance query expected — order.ListPendingConsolidation excludes it before any TronGrid call
	tc := mock.start(t)

	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	got, err := ListPendingConsolidation(context.Background(), deps, walletID)
	if err != nil {
		t.Fatalf("ListPendingConsolidation() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty", got)
	}
}
