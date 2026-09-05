package order

import (
	"context"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// SumConfirmedAmountsForOrders is SumConfirmedAmount's batched sibling —
// one query for many orders instead of one query per order, for
// internal/admin's CSV/Excel export (技術架構設計第11節：avoid turning a
// 10-萬-row export into 10萬 round trips). Orders with no confirmed
// incoming_transactions rows are simply absent from the returned map;
// callers should treat a missing key as 0.
func SumConfirmedAmountsForOrders(ctx context.Context, pool *store.Pool, orderIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT order_id, COALESCE(SUM(amount), 0)
		FROM incoming_transactions
		WHERE confirmed AND order_id = ANY($1)
		GROUP BY order_id
	`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orderID int64
		var sum int64
		if err := rows.Scan(&orderID, &sum); err != nil {
			return nil, err
		}
		out[orderID] = sum
	}
	return out, rows.Err()
}

// LatestTransitionTimes returns, for each of orderIDs, the created_at of
// its most recent order_state_transitions row — used by the CSV/Excel
// export's「完成時間」column (技術架構設計第11節). Meaningful only for orders
// currently in a terminal status (see IsTerminalStatus); callers decide
// whether to display it, this function just returns the raw timestamps.
// Orders with no transition rows are absent from the returned map.
func LatestTransitionTimes(ctx context.Context, pool *store.Pool, orderIDs []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT order_id, MAX(created_at)
		FROM order_state_transitions
		WHERE order_id = ANY($1)
		GROUP BY order_id
	`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orderID int64
		var at time.Time
		if err := rows.Scan(&orderID, &at); err != nil {
			return nil, err
		}
		out[orderID] = at
	}
	return out, rows.Err()
}

// IsTerminalStatus reports whether s is one of the statuses that trigger a
// webhook_deliveries INSERT on entry (第8節「觸發整合」) — the same set
// order.go's terminalStatuses map already defines, exported here as a
// function rather than exporting the map itself so callers can't mutate it.
func IsTerminalStatus(s Status) bool {
	return terminalStatuses[s]
}
