package consolidation

import (
	"context"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// ConsolidationItem is one consolidation_items row, for internal/admin's
// order detail page (技術架構設計第11節「訂單明細頁」：「若已歸集則附對應
// consolidation_items記錄」).
type ConsolidationItem struct {
	ID              int64
	BatchID         int64
	Amount          int64
	TxHash          string
	BroadcastStatus string
	ErrorDetail     *string
	CreatedAt       time.Time
}

// FeeTopupItem is one fee_topup_items row, for the same page (「若該地址曾被儲值
// 過TRX手續費則附對應fee_topup_items記錄」).
type FeeTopupItem struct {
	ID              int64
	BatchID         int64
	Amount          int64
	TxHash          string
	BroadcastStatus string
	ErrorDetail     *string
	CreatedAt       time.Time
}

// ItemsForOrder returns orderID's consolidation and fee-topup history.
// Both tables already carry an (order_id, created_at DESC) index built for
// exactly this lookup (0007_consolidation.up.sql).
func ItemsForOrder(ctx context.Context, pool *store.Pool, orderID int64) ([]ConsolidationItem, []FeeTopupItem, error) {
	items, err := listConsolidationItemsForOrder(ctx, pool, orderID)
	if err != nil {
		return nil, nil, err
	}
	feeTopups, err := listFeeTopupItemsForOrder(ctx, pool, orderID)
	if err != nil {
		return nil, nil, err
	}
	return items, feeTopups, nil
}

func listConsolidationItemsForOrder(ctx context.Context, pool *store.Pool, orderID int64) ([]ConsolidationItem, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, batch_id, amount, tx_hash, broadcast_status, error_detail, created_at
		FROM consolidation_items
		WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConsolidationItem
	for rows.Next() {
		var it ConsolidationItem
		if err := rows.Scan(&it.ID, &it.BatchID, &it.Amount, &it.TxHash, &it.BroadcastStatus, &it.ErrorDetail, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func listFeeTopupItemsForOrder(ctx context.Context, pool *store.Pool, orderID int64) ([]FeeTopupItem, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, batch_id, amount, tx_hash, broadcast_status, error_detail, created_at
		FROM fee_topup_items
		WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeeTopupItem
	for rows.Next() {
		var it FeeTopupItem
		if err := rows.Scan(&it.ID, &it.BatchID, &it.Amount, &it.TxHash, &it.BroadcastStatus, &it.ErrorDetail, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
