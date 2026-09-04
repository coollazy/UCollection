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
	_, err = deps.Pool.Exec(ctx, `
		UPDATE scan_checkpoint
		SET last_synced_block_timestamp = GREATEST(last_synced_block_timestamp, $1), last_synced_at = now(), updated_at = now()
		WHERE id = 1
	`, newCheckpoint)
	return err
}

// handleDetectedEvent matches one Transfer event against orders.address and
// records it. Events not addressed to any of our orders are silently
// ignored (累加金額不分入帳來源以外的其他USDT轉帳，見技術架構設計第4節).
func handleDetectedEvent(ctx context.Context, pool *store.Pool, ev tronclient.Event) error {
	var orderID int64
	err := pool.QueryRow(ctx, `SELECT id FROM orders WHERE address = $1`, ev.To).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	var inserted bool
	err = pool.QueryRow(ctx, `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, $2, $3, $4, false, $5)
		ON CONFLICT (tx_hash, log_index) DO NOTHING
		RETURNING true
	`, orderID, ev.TransactionID, ev.EventIndex, ev.Value, ev.BlockNumber).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already recorded (pagination overlap or repeat poll) — not new, nothing to evaluate
	}
	if err != nil {
		return err
	}

	if err := order.EvaluateConfirming(ctx, pool, orderID); err != nil && !errors.Is(err, order.ErrTransitionNotDue) {
		return err
	}
	return nil
}
