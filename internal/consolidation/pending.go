package consolidation

import (
	"context"

	"github.com/coollazy/UCollection/internal/order"
)

// PendingEntry is one row of the pending-consolidation list.
type PendingEntry struct {
	OrderID      int64
	Address      string
	Status       order.Status
	TargetAmount int64
	Balance      int64
	OverTarget   bool
}

// ListPendingConsolidation implements 技術架構設計第10節「待歸集地址列表」步驟2-4:
// candidate orders (order.ListPendingConsolidation) filtered down to those
// with a live on-chain USDT balance > 0 (只顯示餘額>0的地址), each carrying an
// OverTarget flag when a COMPLETED order's on-chain balance exceeds its
// original target_amount (步驟4「標示餘額超出訂單原定金額提醒」).
func ListPendingConsolidation(ctx context.Context, deps Deps, masterWalletID int64) ([]PendingEntry, error) {
	candidates, err := order.ListPendingConsolidation(ctx, deps.Pool, masterWalletID)
	if err != nil {
		return nil, err
	}

	var out []PendingEntry
	for _, o := range candidates {
		balance, err := deps.TronClient.TRC20Balance(ctx, o.Address, deps.USDTContractAddress)
		if err != nil {
			return nil, err
		}
		if balance <= 0 {
			continue
		}
		out = append(out, PendingEntry{
			OrderID:      o.ID,
			Address:      o.Address,
			Status:       o.Status,
			TargetAmount: o.TargetAmount,
			Balance:      balance,
			OverTarget:   o.Status == order.StatusCompleted && balance > o.TargetAmount,
		})
	}
	return out, nil
}
