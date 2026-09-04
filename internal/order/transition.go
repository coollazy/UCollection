package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// ErrTransitionNotDue is returned when the lock was acquired but decide
// reported the business condition isn't met yet (自動路徑呼叫方視為正常冪等情況，
// 見技術架構設計第5節「回傳契約」).
var ErrTransitionNotDue = errors.New("order: transition condition not met")

// ErrInvalidTransition is returned when the order's current status is not
// in the caller's allowed-from whitelist.
var ErrInvalidTransition = errors.New("order: current status not in allowed-from whitelist")

// ErrOrderNotFound is returned when orderID does not exist.
var ErrOrderNotFound = errors.New("order: not found")

// decideFunc inspects the locked current order (and may run further reads
// within the same tx, e.g. summing incoming_transactions) and decides
// whether/where to transition. snapshot is stored verbatim into
// order_state_transitions.decision_snapshot for audit purposes (第5節「稽核可
// 追溯性」).
type decideFunc func(ctx context.Context, tx pgx.Tx, current Order) (to Status, ok bool, snapshot map[string]any, err error)

// transition implements the shared "lock -> judge -> UPDATE -> COMMIT"
// skeleton (技術架構設計第5節「原子性做法」). It is used by both the automatic
// evaluation functions (evaluate.go) and the manual override path
// (manual.go); the only difference between the two is what allowedFrom and
// decide they pass in.
func transition(ctx context.Context, pool *store.Pool, orderID int64, allowedFrom []Status, decide decideFunc, changedBy, note string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("order: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1 FOR UPDATE`, orderID)
	current, err := scanOrder(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		return fmt.Errorf("order: lock order %d: %w", orderID, err)
	}

	if !statusIn(current.Status, allowedFrom) {
		return fmt.Errorf("%w: order %d is %s, allowed from %v", ErrInvalidTransition, orderID, current.Status, allowedFrom)
	}

	to, ok, snapshot, err := decide(ctx, tx, current)
	if err != nil {
		return fmt.Errorf("order: decide transition for order %d: %w", orderID, err)
	}
	if !ok {
		return ErrTransitionNotDue
	}

	if to == StatusConfirming {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = $1, confirming_at = now(), updated_at = now() WHERE id = $2`, to, orderID); err != nil {
			return fmt.Errorf("order: update order %d: %w", orderID, err)
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = $1, updated_at = now() WHERE id = $2`, to, orderID); err != nil {
			return fmt.Errorf("order: update order %d: %w", orderID, err)
		}
	}

	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("order: marshal decision snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO order_state_transitions (order_id, from_status, to_status, changed_by, note, decision_snapshot)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, orderID, current.Status, to, changedBy, note, snapshotJSON); err != nil {
		return fmt.Errorf("order: insert order_state_transitions: %w", err)
	}

	if terminalStatuses[to] {
		confirmedAmount, err := sumConfirmedAmount(ctx, tx, orderID)
		if err != nil {
			return fmt.Errorf("order: sum confirmed amount for webhook payload: %w", err)
		}
		if err := insertWebhookDelivery(ctx, tx, current, to, confirmedAmount); err != nil {
			return fmt.Errorf("order: insert webhook_deliveries: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("order: commit: %w", err)
	}
	return nil
}

func statusIn(s Status, list []Status) bool {
	for _, v := range list {
		if s == v {
			return true
		}
	}
	return false
}

// querier is satisfied by both pgx.Tx (used mid-transaction, see transition
// above) and *store.Pool (used for standalone reads, see query.go) — lets
// read helpers like sumConfirmedAmount be shared by both call sites instead
// of duplicating the SQL.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func sumConfirmedAmount(ctx context.Context, q querier, orderID int64) (int64, error) {
	var sum int64
	err := q.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM incoming_transactions WHERE order_id = $1 AND confirmed = true`, orderID).Scan(&sum)
	return sum, err
}

// webhookPayload matches 技術架構設計第8節「Payload」. Amount fields are
// strings, matching 第7節 API 模組同一個先例（避免JSON數值精度問題）.
type webhookPayload struct {
	EventID              string `json:"event_id"`
	EventType            string `json:"event_type"`
	OrderID              string `json:"order_id"`
	MerchantOrderNo      string `json:"merchant_order_no"`
	Status               string `json:"status"`
	TargetAmount         string `json:"target_amount"`
	TotalConfirmedAmount string `json:"total_confirmed_amount"`
	OccurredAt           string `json:"occurred_at"`
}

// insertWebhookDelivery inserts one webhook_deliveries row for a terminal
// state entry (技術架構設計第8節「觸發整合」). It reads system_params within the
// same transaction to decide the initial status: 'awaiting_config' if
// webhook_url/webhook_secret aren't both set yet, 'pending' otherwise (第8節
// 「未設定URL/secret的邊界」). The internal/webhook worker that actually sends
// these doesn't exist yet — this only makes sure the outbox row is correct
// when that module is built.
func insertWebhookDelivery(ctx context.Context, tx pgx.Tx, o Order, to Status, confirmedAmount int64) error {
	var webhookURL, webhookSecret *string
	if err := tx.QueryRow(ctx, `SELECT webhook_url, webhook_secret FROM system_params WHERE id = 1`).Scan(&webhookURL, &webhookSecret); err != nil {
		return fmt.Errorf("read system_params: %w", err)
	}

	status := "awaiting_config"
	var nextRetryAt *time.Time
	if webhookURL != nil && *webhookURL != "" && webhookSecret != nil && *webhookSecret != "" {
		status = "pending"
		now := time.Now()
		nextRetryAt = &now
	}

	eventID, err := randomToken()
	if err != nil {
		return fmt.Errorf("generate event_id: %w", err)
	}

	occurredAt := time.Now()
	payload, err := json.Marshal(webhookPayload{
		EventID:              eventID,
		EventType:            "ORDER_" + string(to),
		OrderID:              fmt.Sprintf("%d", o.ID),
		MerchantOrderNo:      o.MerchantOrderNo,
		Status:               string(to),
		TargetAmount:         fmt.Sprintf("%d", o.TargetAmount),
		TotalConfirmedAmount: fmt.Sprintf("%d", confirmedAmount),
		OccurredAt:           occurredAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO webhook_deliveries (event_id, order_id, event_type, payload, status, next_retry_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventID, o.ID, "ORDER_"+string(to), payload, status, nextRetryAt)
	return err
}
