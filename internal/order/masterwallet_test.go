package order

import (
	"context"
	"errors"
	"testing"
)

func TestActiveMasterWalletID(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	t.Run("none configured", func(t *testing.T) {
		_, err := ActiveMasterWalletID(ctx, pool)
		if !errors.Is(err, ErrNoActiveMasterWallet) {
			t.Fatalf("err = %v, want ErrNoActiveMasterWallet", err)
		}
	})

	activeID := newMasterWallet(t, pool, testXpub)
	t.Run("one active", func(t *testing.T) {
		got, err := ActiveMasterWalletID(ctx, pool)
		if err != nil {
			t.Fatalf("ActiveMasterWalletID() error = %v", err)
		}
		if got != activeID {
			t.Errorf("got %d, want %d", got, activeID)
		}
	})

	if _, err := pool.Exec(ctx, `UPDATE master_wallets SET status = 'inactive' WHERE id = $1`, activeID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	t.Run("deactivated goes back to none", func(t *testing.T) {
		_, err := ActiveMasterWalletID(ctx, pool)
		if !errors.Is(err, ErrNoActiveMasterWallet) {
			t.Fatalf("err = %v, want ErrNoActiveMasterWallet", err)
		}
	})
}
