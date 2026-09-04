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

// ListPendingConsolidation returns every order under masterWalletID that
// has never been consolidated (技術架構設計第10節「待歸集地址列表」step 2). This
// is the bounded query the doc contrasts with the already-rejected
// "poll every historical address" approach (第4節) — scope is limited to a
// single master wallet's not-yet-consolidated orders, which shrinks as the
// operator consolidates more. Callers (internal/consolidation) are
// responsible for the live on-chain balance lookup and the balance>0 filter
// (技術架構設計第10節 step 3) — this function only knows about order state.
func ListPendingConsolidation(ctx context.Context, pool *store.Pool, masterWalletID int64) ([]Order, error) {
	rows, err := pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM orders
		WHERE master_wallet_id = $1 AND consolidation_status = $2
		ORDER BY id
	`, masterWalletID, ConsolidationNotConsolidated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// MarkConsolidated permanently flags orderID as consolidated (技術架構設計第
// 10節「`consolidation_status`是永久性旗標」) once a broadcast has actually
// succeeded on chain. There is no path back to not_consolidated.
func MarkConsolidated(ctx context.Context, pool *store.Pool, orderID int64) error {
	_, err := pool.Exec(ctx, `
		UPDATE orders SET consolidation_status = $2, updated_at = now() WHERE id = $1
	`, orderID, ConsolidationConsolidated)
	return err
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
