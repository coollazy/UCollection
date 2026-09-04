package order

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// GetByID reads one order by its system ID. Returns ErrOrderNotFound if it
// doesn't exist.
func GetByID(ctx context.Context, pool *store.Pool, id int64) (Order, error) {
	row := pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1`, id)
	o, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return o, err
}

// GetByMerchantOrderNo reads one order by merchant_order_no (unique per
// 第2節). Returns ErrOrderNotFound if it doesn't exist.
func GetByMerchantOrderNo(ctx context.Context, pool *store.Pool, merchantOrderNo string) (Order, error) {
	row := pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_order_no = $1`, merchantOrderNo)
	o, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return o, err
}

// DetectedAmount sums every incoming_transactions row for orderID
// regardless of confirmed status (技術架構設計第7節「查詢API」：累計已偵測金額).
func DetectedAmount(ctx context.Context, pool *store.Pool, orderID int64) (int64, error) {
	var sum int64
	err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM incoming_transactions WHERE order_id = $1`, orderID).Scan(&sum)
	return sum, err
}

// SumConfirmedAmount sums confirmed=true incoming_transactions rows for
// orderID (技術架構設計第7節「查詢API」：累計已確認金額). Exported for
// internal/api's query handler; shares the same query as the internal
// evaluate/transition logic via the querier interface (見transition.go).
func SumConfirmedAmount(ctx context.Context, pool *store.Pool, orderID int64) (int64, error) {
	return sumConfirmedAmount(ctx, pool, orderID)
}

// Transition is one row of order_state_transitions (技術架構設計第7節「查詢
// API」：order_state_transitions完整狀態變更歷史).
type Transition struct {
	FromStatus string
	ToStatus   string
	ChangedBy  string
	Note       string
	CreatedAt  time.Time
}

// Transitions returns orderID's complete state-change history, oldest
// first.
func Transitions(ctx context.Context, pool *store.Pool, orderID int64) ([]Transition, error) {
	rows, err := pool.Query(ctx, `
		SELECT from_status, to_status, changed_by, coalesce(note, ''), created_at
		FROM order_state_transitions
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transition
	for rows.Next() {
		var t Transition
		if err := rows.Scan(&t.FromStatus, &t.ToStatus, &t.ChangedBy, &t.Note, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
