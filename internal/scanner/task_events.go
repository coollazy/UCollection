package scanner

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// runEventsTask is 任務2「事件掃描迴圈」(技術架構設計第4節): the primary
// on-chain monitor. It scans only_confirmed=false Transfer events (near
// real-time, ~23s latency per 驗證結論-02), matches them against
// orders.address, and records new incoming_transactions.
func runEventsTask(ctx context.Context, deps Deps) error {
	for {
		interval := nextEventsInterval(ctx, deps.Pool)
		if err := scanEventsOnce(ctx, deps); err != nil {
			log.Printf("scanner: events task: scan cycle failed, will retry: %v", err)
		}

		if !sleepOrDone(ctx, interval) {
			return nil
		}
	}
}

// nextEventsInterval picks the poll interval per 技術架構設計第4節「任務2」:
// faster while there are orders that could still receive/need a payment,
// slower otherwise — never stopped, checkpoint keeps advancing either way.
func nextEventsInterval(ctx context.Context, pool *store.Pool) time.Duration {
	var active bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE status IN ('PENDING', 'CONFIRMING'))`).Scan(&active)
	if err != nil {
		log.Printf("scanner: events task: check active orders: %v", err)
		return eventsTickerIdle
	}
	if active {
		return eventsTickerActive
	}
	return eventsTickerIdle
}

func scanEventsOnce(ctx context.Context, deps Deps) error {
	var checkpoint int64
	err := deps.Pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&checkpoint)
	if err != nil {
		return err
	}

	maxSeen := checkpoint
	fingerprint := ""
	for {
		page, err := deps.TronClient.ContractEvents(ctx, deps.ContractAddress, tronclient.EventsQuery{
			EventName:         contractEventName,
			OnlyConfirmed:     false,
			MinBlockTimestamp: checkpoint,
			Fingerprint:       fingerprint,
			Limit:             pageLimit,
		})
		if err != nil {
			return err
		}

		for _, ev := range page.Events {
			if ev.BlockTimestamp > maxSeen {
				maxSeen = ev.BlockTimestamp
			}
			if err := handleDetectedEvent(ctx, deps.Pool, ev); err != nil {
				log.Printf("scanner: events task: handle event %s: %v", ev.TransactionID, err)
			}
		}

		if page.NextFingerprint == "" {
			break
		}
		fingerprint = page.NextFingerprint
	}

	// USDT is high-traffic, so maxSeen tracks close to "now" every poll even
	// when nothing matched our own addresses this round — checkpoint won't
	// stall waiting for our own traffic specifically.
	newCheckpoint := maxSeen - checkpointSafetyBufferMs
	if newCheckpoint < checkpoint {
		logCheckpointAnomaly(ctx, deps.Pool, "last_synced_block_timestamp", checkpoint, newCheckpoint)
	}
	_, err = deps.Pool.Exec(ctx, `
		UPDATE scan_checkpoint
		SET last_synced_block_timestamp = GREATEST(last_synced_block_timestamp, $1), last_synced_at = now(), updated_at = now()
		WHERE id = 1
	`, newCheckpoint)
	return err
}

// handleDetectedEvent matches one Transfer event against orders.address,
// records it, and (if it was actually new) evaluates whether the order can
// advance to CONFIRMING. Events not addressed to any of our orders are
// silently ignored (累加金額不分入帳來源以外的其他USDT轉帳，見技術架構設計第4節).
func handleDetectedEvent(ctx context.Context, pool *store.Pool, ev tronclient.Event) error {
	orderID, inserted, err := mergeIncomingTransaction(ctx, pool, ev)
	if err != nil || !inserted {
		return err
	}

	if err := order.EvaluateConfirming(ctx, pool, orderID); err != nil && !errors.Is(err, order.ErrTransitionNotDue) {
		return err
	}
	return nil
}

// mergeIncomingTransaction looks up the order owning ev.To and records the
// event into incoming_transactions if it isn't already there, without
// evaluating any state transition — the piece of handleDetectedEvent that
// internal/admin's reverify handler also needs (技術架構設計第11節「手動重新檢查
// 此地址」：沿用第4節任務2完全相同的端點與資料結構、同一去重鍵，但「本操作本身不觸發任何
// 自動狀態轉換」, see scanner.ReverifyOrder in reverify.go). inserted is false
// both when ev.To matches no order and when the row already existed
// (pagination overlap or repeat poll) — either way there's nothing new to
// evaluate.
func mergeIncomingTransaction(ctx context.Context, pool *store.Pool, ev tronclient.Event) (orderID int64, inserted bool, err error) {
	err = pool.QueryRow(ctx, `SELECT id FROM orders WHERE address = $1`, ev.To).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, $2, $3, $4, false, $5)
		ON CONFLICT (tx_hash, log_index) DO NOTHING
		RETURNING true
	`, orderID, ev.TransactionID, ev.EventIndex, ev.Value, ev.BlockNumber).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderID, false, nil // already recorded
	}
	if err != nil {
		return 0, false, err
	}
	return orderID, true, nil
}
