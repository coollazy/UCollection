package consolidation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// masterWalletOption is the minimal shape the pending-list page needs to
// offer a wallet picker (技術架構設計第10節「待歸集地址列表」步驟1: active or
// inactive wallets can both be picked).
type masterWalletOption struct {
	ID     int64
	Status string
}

// listMasterWallets queries master_wallets directly rather than going
// through a dedicated package — this project has no single owner for that
// table (internal/order and internal/api both query it ad hoc for their
// own narrow needs already), so this follows the same established pattern
// rather than introducing a new internal/masterwallet package for one
// small query.
func listMasterWallets(ctx context.Context, pool *store.Pool) ([]masterWalletOption, error) {
	rows, err := pool.Query(ctx, `SELECT id, status FROM master_wallets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []masterWalletOption
	for rows.Next() {
		var w masterWalletOption
		if err := rows.Scan(&w.ID, &w.Status); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// masterWallet is the fuller shape sign.go needs: the xpub is what the
// browser's locally-derived xpub gets compared against (技術架構設計第10節「商戶
// 輸入助記詞後，前端本地衍生xpub，與該代收主錢包已存的master_wallets.xpub...逐字比對」)
// — public key material, not a secret, safe to embed directly in the page.
type masterWallet struct {
	ID     int64
	Xpub   string
	Status string
}

func getMasterWallet(ctx context.Context, pool *store.Pool, id int64) (masterWallet, error) {
	var w masterWallet
	err := pool.QueryRow(ctx, `SELECT id, xpub, status FROM master_wallets WHERE id = $1`, id).
		Scan(&w.ID, &w.Xpub, &w.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return masterWallet{}, errRowNotFound
	}
	return w, err
}
