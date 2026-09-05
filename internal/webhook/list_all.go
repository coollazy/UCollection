package webhook

import (
	"context"
	"fmt"
	"strings"

	"github.com/coollazy/UCollection/internal/store"
)

// ListFilter narrows down ListAll (技術架構設計第11節「通知歷史查詢」：可依訂單、
// event_type、status篩選). All fields are optional (zero value = no filter
// on that column).
type ListFilter struct {
	OrderID           *int64
	EventType, Status string
	Offset, Limit     int
}

// ListAll returns a page of webhook_deliveries rows across ALL orders
// matching f (newest first) with their attempt history, plus the total row
// count across all pages. Same shape as ListForOrder — DeliveryWithAttempts
// was already designed to be shared by both (see its doc comment) — this
// is just ListForOrder's SQL with a dynamic WHERE instead of a fixed
// order_id, for internal/admin's cross-order notification-history page.
func ListAll(ctx context.Context, pool *store.Pool, f ListFilter) ([]DeliveryWithAttempts, int, error) {
	var where []string
	var args []any

	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if f.OrderID != nil {
		where = append(where, "order_id = "+arg(*f.OrderID))
	}
	if f.EventType != "" {
		where = append(where, "event_type = "+arg(f.EventType))
	}
	if f.Status != "" {
		where = append(where, "status = "+arg(f.Status))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT id, order_id, event_id, event_type, status, attempt_count, next_retry_at, created_at, COUNT(*) OVER() AS total
		FROM webhook_deliveries
		%s
		ORDER BY id DESC
		LIMIT %s OFFSET %s
	`, whereClause, arg(limit), arg(f.Offset))

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}

	var deliveries []DeliveryWithAttempts
	var total int
	for rows.Next() {
		var d DeliveryWithAttempts
		if err := rows.Scan(&d.ID, &d.OrderID, &d.EventID, &d.EventType, &d.Status, &d.AttemptCount, &d.NextRetryAt, &d.CreatedAt, &total); err != nil {
			rows.Close()
			return nil, 0, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()

	for i := range deliveries {
		attempts, err := listAttempts(ctx, pool, deliveries[i].ID)
		if err != nil {
			return nil, 0, err
		}
		deliveries[i].Attempts = attempts
	}
	return deliveries, total, nil
}
