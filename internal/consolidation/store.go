package consolidation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

var errRowNotFound = errors.New("consolidation: row not found")

type consolidationBatch struct {
	ID                 int64
	MasterWalletID     int64
	DestinationAddress string
}

func loadConsolidationBatch(ctx context.Context, pool *store.Pool, id int64) (consolidationBatch, error) {
	var b consolidationBatch
	err := pool.QueryRow(ctx, `
		SELECT id, master_wallet_id, destination_address FROM consolidation_batches WHERE id = $1
	`, id).Scan(&b.ID, &b.MasterWalletID, &b.DestinationAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return consolidationBatch{}, errRowNotFound
	}
	return b, err
}

type feeTopupBatch struct {
	ID               int64
	MasterWalletID   int64
	FeeSource        string
	FeeSourceAddress string
}

func loadFeeTopupBatch(ctx context.Context, pool *store.Pool, id int64) (feeTopupBatch, error) {
	var b feeTopupBatch
	err := pool.QueryRow(ctx, `
		SELECT id, master_wallet_id, fee_source, fee_source_address FROM fee_topup_batches WHERE id = $1
	`, id).Scan(&b.ID, &b.MasterWalletID, &b.FeeSource, &b.FeeSourceAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return feeTopupBatch{}, errRowNotFound
	}
	return b, err
}

type consolidationItem struct {
	ID                 int64
	OrderID            int64
	TxHash             string
	BroadcastStatus    string
	DestinationAddress string
}

func loadConsolidationItem(ctx context.Context, pool *store.Pool, id int64) (consolidationItem, error) {
	var it consolidationItem
	err := pool.QueryRow(ctx, `
		SELECT ci.id, ci.order_id, ci.tx_hash, ci.broadcast_status, cb.destination_address
		FROM consolidation_items ci
		JOIN consolidation_batches cb ON cb.id = ci.batch_id
		WHERE ci.id = $1
	`, id).Scan(&it.ID, &it.OrderID, &it.TxHash, &it.BroadcastStatus, &it.DestinationAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return consolidationItem{}, errRowNotFound
	}
	return it, err
}

type feeTopupItem struct {
	ID              int64
	OrderID         int64
	TxHash          string
	BroadcastStatus string
	// FeeSourceAddress 來自所屬 fee_topup_batches（透過 loadFeeTopupItem 的 JOIN
	// 帶出），供廣播成功後自動存入手續費來源地址簿（需求書5.9 v0.39）。
	FeeSourceAddress string
}

func loadFeeTopupItem(ctx context.Context, pool *store.Pool, id int64) (feeTopupItem, error) {
	var it feeTopupItem
	err := pool.QueryRow(ctx, `
		SELECT i.id, i.order_id, i.tx_hash, i.broadcast_status, b.fee_source_address
		FROM fee_topup_items i
		JOIN fee_topup_batches b ON b.id = i.batch_id
		WHERE i.id = $1
	`, id).Scan(&it.ID, &it.OrderID, &it.TxHash, &it.BroadcastStatus, &it.FeeSourceAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return feeTopupItem{}, errRowNotFound
	}
	return it, err
}

// extractTxIDFromJSON pulls the "txID" field out of an opaque transaction
// JSON blob — used to defensively verify the transaction the browser signed
// and sent back to /broadcast really is the one this server prepared,
// before ever calling TronGrid's broadcasttransaction.
func extractTxIDFromJSON(raw json.RawMessage) (string, error) {
	var t struct {
		TxID string `json:"txID"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", err
	}
	return t.TxID, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
