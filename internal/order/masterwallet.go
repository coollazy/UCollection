package order

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// ErrNoActiveMasterWallet is returned when no master_wallets row has
// status='active' (技術架構設計第11節「代收主錢包導引Gate」).
var ErrNoActiveMasterWallet = errors.New("order: no active master wallet configured")

// ActiveMasterWalletID returns the id of the single active master wallet
// (技術架構設計第2節: at most one row has status='active' at a time — enforced
// by internal/admin's master-wallet-switch handler, not a DB constraint).
// Returns ErrNoActiveMasterWallet if none exists yet.
func ActiveMasterWalletID(ctx context.Context, pool *store.Pool) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM master_wallets WHERE status = 'active' ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoActiveMasterWallet
	}
	return id, err
}
