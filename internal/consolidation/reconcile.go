package consolidation

import (
	"context"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// ReconcileBroadcasting implements the lazy reconcile pass from 技術架構設計
// 第10節「廣播結果判斷與部分失敗」: for every item still stuck in
// 'broadcasting' (a broadcast whose HTTP response never came back — see
// broadcast.go's decideBroadcastOutcome), query TransactionInfo and resolve
// it to success/failed based on what's actually on chain. Not found yet
// (TransactionInfo.Found=false) leaves the item as 'broadcasting' — it's
// still genuinely unknown, not a failure.
//
// masterWalletID scopes the query to one master wallet; nil scopes it to
// every master wallet, matching 技術架構設計第10節「尚未選定代收主錢包時的查詢範
// 圍」(the pending list page hasn't narrowed to one wallet yet).
//
// This is intentionally called on page load (a "lazy" reconcile), not from
// a background ticker — 技術架構設計第10節 explicitly rejects a permanent
// polling loop here, since manual consolidation is low-frequency human-
// operated work (unlike 第8節 webhook worker's always-on queue).
func ReconcileBroadcasting(ctx context.Context, deps Deps, masterWalletID *int64) error {
	if err := reconcileConsolidationItems(ctx, deps, masterWalletID); err != nil {
		return err
	}
	return reconcileFeeTopupItems(ctx, deps, masterWalletID)
}

type broadcastingItem struct {
	ID      int64
	OrderID int64
	TxHash  string
}

func reconcileConsolidationItems(ctx context.Context, deps Deps, masterWalletID *int64) error {
	items, err := queryBroadcastingItems(ctx, deps, `
		SELECT ci.id, ci.order_id, ci.tx_hash
		FROM consolidation_items ci
		JOIN consolidation_batches cb ON cb.id = ci.batch_id
		WHERE ci.broadcast_status = 'broadcasting'
	`, "cb.master_wallet_id", masterWalletID)
	if err != nil {
		return err
	}

	for _, it := range items {
		info, err := deps.TronClient.TransactionInfo(ctx, it.TxHash)
		if err != nil {
			return err
		}
		if !info.Found {
			continue
		}

		// USDT consolidation is always a TriggerSmartContract call — success
		// is defined solely by receipt.result == "SUCCESS" (技術架構設計第10節).
		status, errorDetail := reconcileOutcome(info, info.Receipt.Result == "SUCCESS")
		if err := updateConsolidationItemStatus(ctx, deps, it.ID, status, errorDetail); err != nil {
			return err
		}
		if status == "success" {
			if err := order.MarkConsolidated(ctx, deps.Pool, it.OrderID); err != nil {
				return err
			}
		}
	}
	return nil
}

func reconcileFeeTopupItems(ctx context.Context, deps Deps, masterWalletID *int64) error {
	items, err := queryBroadcastingItems(ctx, deps, `
		SELECT fi.id, fi.order_id, fi.tx_hash
		FROM fee_topup_items fi
		JOIN fee_topup_batches fb ON fb.id = fi.batch_id
		WHERE fi.broadcast_status = 'broadcasting'
	`, "fb.master_wallet_id", masterWalletID)
	if err != nil {
		return err
	}

	for _, it := range items {
		info, err := deps.TronClient.TransactionInfo(ctx, it.TxHash)
		if err != nil {
			return err
		}
		if !info.Found {
			continue
		}

		// TransferContract has no receipt.result field at all (驗證結論-06
		// 附加測試) — success is "found on chain with a block number".
		status, errorDetail := reconcileOutcome(info, info.BlockNumber > 0)
		if err := updateFeeTopupItemStatus(ctx, deps, it.ID, status, errorDetail); err != nil {
			return err
		}
	}
	return nil
}

func reconcileOutcome(info tronclient.TransactionInfo, success bool) (status, errorDetail string) {
	if success {
		return "success", ""
	}
	if info.Receipt.Result != "" {
		return "failed", info.Receipt.Result
	}
	return "failed", "transaction found on chain but did not succeed"
}

func queryBroadcastingItems(ctx context.Context, deps Deps, baseQuery, walletColumn string, masterWalletID *int64) ([]broadcastingItem, error) {
	query := baseQuery
	args := []any{}
	if masterWalletID != nil {
		query += " AND " + walletColumn + " = $1"
		args = append(args, *masterWalletID)
	}

	rows, err := deps.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []broadcastingItem
	for rows.Next() {
		var it broadcastingItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.TxHash); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func updateConsolidationItemStatus(ctx context.Context, deps Deps, id int64, status, errorDetail string) error {
	_, err := deps.Pool.Exec(ctx, `
		UPDATE consolidation_items SET broadcast_status = $2, error_detail = $3, updated_at = now() WHERE id = $1
	`, id, status, nullIfEmpty(errorDetail))
	return err
}

func updateFeeTopupItemStatus(ctx context.Context, deps Deps, id int64, status, errorDetail string) error {
	_, err := deps.Pool.Exec(ctx, `
		UPDATE fee_topup_items SET broadcast_status = $2, error_detail = $3, updated_at = now() WHERE id = $1
	`, id, status, nullIfEmpty(errorDetail))
	return err
}
