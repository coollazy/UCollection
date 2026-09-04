package order

import (
	"context"
	"testing"
)

func TestListPendingConsolidation(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)
	otherWalletID := newMasterWallet(t, pool, testXpub) // same xpub is fine (no uniqueness constraint); bump its last_derived_index below so it doesn't derive the same address as walletID's index 1
	if _, err := pool.Exec(context.Background(), `UPDATE master_wallets SET last_derived_index = 50 WHERE id = $1`, otherWalletID); err != nil {
		t.Fatalf("bump last_derived_index: %v", err)
	}

	o1, err := CreateOrder(context.Background(), pool, defaultParams("pc-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	o2, err := CreateOrder(context.Background(), pool, defaultParams("pc-2", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	if _, err := CreateOrder(context.Background(), pool, defaultParams("pc-other-wallet", otherWalletID)); err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	if err := MarkConsolidated(context.Background(), pool, o2.ID); err != nil {
		t.Fatalf("MarkConsolidated() error = %v", err)
	}

	got, err := ListPendingConsolidation(context.Background(), pool, walletID)
	if err != nil {
		t.Fatalf("ListPendingConsolidation() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != o1.ID {
		t.Fatalf("ListPendingConsolidation() = %+v, want only order %d (o2 consolidated, other-wallet order excluded)", got, o1.ID)
	}
}

func TestMarkConsolidated_IsPermanent(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	o, err := CreateOrder(context.Background(), pool, defaultParams("mc-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	if o.ConsolidationStatus != ConsolidationNotConsolidated {
		t.Fatalf("initial ConsolidationStatus = %q, want %q", o.ConsolidationStatus, ConsolidationNotConsolidated)
	}

	if err := MarkConsolidated(context.Background(), pool, o.ID); err != nil {
		t.Fatalf("MarkConsolidated() error = %v", err)
	}

	got, err := GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ConsolidationStatus != ConsolidationConsolidated {
		t.Fatalf("ConsolidationStatus = %q, want %q", got.ConsolidationStatus, ConsolidationConsolidated)
	}

	// Confirm it no longer shows up as pending.
	pending, err := ListPendingConsolidation(context.Background(), pool, walletID)
	if err != nil {
		t.Fatalf("ListPendingConsolidation() error = %v", err)
	}
	for _, p := range pending {
		if p.ID == o.ID {
			t.Fatalf("consolidated order %d still appears in ListPendingConsolidation()", o.ID)
		}
	}
}
