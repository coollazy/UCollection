package order

import (
	"context"
	"errors"
	"testing"
)

func TestGetByPublicToken(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	created, err := CreateOrder(context.Background(), pool, defaultParams("order-token-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	if created.PublicToken == "" {
		t.Fatal("created order has empty public_token")
	}

	got, err := GetByPublicToken(context.Background(), pool, created.PublicToken)
	if err != nil {
		t.Fatalf("GetByPublicToken() error = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetByPublicToken() ID = %d, want %d", got.ID, created.ID)
	}
	if got.Address != created.Address {
		t.Errorf("GetByPublicToken() Address = %q, want %q", got.Address, created.Address)
	}
}

func TestGetByPublicToken_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	_, err := GetByPublicToken(context.Background(), pool, "this-token-does-not-exist")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("GetByPublicToken() error = %v, want ErrOrderNotFound", err)
	}
}
