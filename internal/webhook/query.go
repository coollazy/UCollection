package webhook

import (
	"context"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// Attempt mirrors one webhook_delivery_attempts row.
type Attempt struct {
	AttemptNo    int
	HTTPStatus   *int
	ResponseBody *string
	AttemptedAt  time.Time
	TriggeredBy  string
}

// DeliveryWithAttempts is one webhook_deliveries row plus its full attempt
// history, for internal/admin's order detail page (技術架構設計第11節「訂單列表
// 與明細」：「對應webhook_deliveries／webhook_delivery_attempts（該訂單的通知記錄，
// 供客服對照）」) and notification-history page (第11節「通知歷史查詢」).
type DeliveryWithAttempts struct {
	ID           int64
	OrderID      int64
	EventID      string
	EventType    string
	Status       string
	AttemptCount int
	NextRetryAt  *time.Time
	CreatedAt    time.Time
	Attempts     []Attempt
}

// ListForOrder returns every webhook_deliveries row for orderID (newest
// first) with its attempts (oldest first).
func ListForOrder(ctx context.Context, pool *store.Pool, orderID int64) ([]DeliveryWithAttempts, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, order_id, event_id, event_type, status, attempt_count, next_retry_at, created_at
		FROM webhook_deliveries
		WHERE order_id = $1
		ORDER BY id DESC
	`, orderID)
	if err != nil {
		return nil, err
	}

	var deliveries []DeliveryWithAttempts
	for rows.Next() {
		var d DeliveryWithAttempts
		if err := rows.Scan(&d.ID, &d.OrderID, &d.EventID, &d.EventType, &d.Status, &d.AttemptCount, &d.NextRetryAt, &d.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for i := range deliveries {
		attempts, err := listAttempts(ctx, pool, deliveries[i].ID)
		if err != nil {
			return nil, err
		}
		deliveries[i].Attempts = attempts
	}
	return deliveries, nil
}

func listAttempts(ctx context.Context, pool *store.Pool, deliveryID int64) ([]Attempt, error) {
	rows, err := pool.Query(ctx, `
		SELECT attempt_no, http_status, response_body, attempted_at, triggered_by
		FROM webhook_delivery_attempts
		WHERE delivery_id = $1
		ORDER BY attempt_no ASC
	`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Attempt
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.AttemptNo, &a.HTTPStatus, &a.ResponseBody, &a.AttemptedAt, &a.TriggeredBy); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
