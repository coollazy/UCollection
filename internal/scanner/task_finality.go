package scanner

import (
	"context"
	"errors"
	"log"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// runFinalityTask is 任務3「最終確定判定」(技術架構設計第4節): re-scans the same
// contract events endpoint with only_confirmed=true and marks matching
// incoming_transactions rows confirmed=true. Runs at a fixed cadence
// (unlike 任務2 it doesn't idle down — whether an order can converge to
// COMPLETED/OVERPAID depends on this task staying current).
//
// Reorg "transaction disappeared from chain" detection (第4節任務3後半段) is
// intentionally not implemented here — it needs TronGrid's
// gettransactioninfobyid, which internal/tronclient doesn't have yet (that
// endpoint is scoped into the internal/consolidation module, see this
// module's plan notes). This is not a money-safety gap: an order whose
// detected transaction never confirms simply stays in CONFIRMING until
// CONFIRMATION_STALLED fires on schedule (task_lifecycle.go), which is the
// documented manual-review fallback anyway (需求書5.2).
func runFinalityTask(ctx context.Context, deps Deps) error {
	for {
		if err := scanFinalityOnce(ctx, deps); err != nil {
			log.Printf("scanner: finality task: scan cycle failed, will retry: %v", err)
		}
		if !sleepOrDone(ctx, finalityTickerPeriod) {
			return nil
		}
	}
}

func scanFinalityOnce(ctx context.Context, deps Deps) error {
	var checkpoint int64
	err := deps.Pool.QueryRow(ctx, `SELECT last_finality_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&checkpoint)
	if err != nil {
		return err
	}

	maxSeen := checkpoint
	fingerprint := ""
	for {
		page, err := deps.TronClient.ContractEvents(ctx, deps.ContractAddress, tronclient.EventsQuery{
			EventName:         contractEventName,
			OnlyConfirmed:     true,
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
			if err := handleFinalizedEvent(ctx, deps.Pool, ev); err != nil {
				log.Printf("scanner: finality task: handle event %s: %v", ev.TransactionID, err)
			}
		}

		if page.NextFingerprint == "" {
			break
		}
		fingerprint = page.NextFingerprint
	}

	// No safety buffer here (unlike 任務2): only_confirmed=true events are
	// already the solidified/final state, so there's nothing to hold back
	// for — this checkpoint is purely a pagination cursor.
	_, err = deps.Pool.Exec(ctx, `
		UPDATE scan_checkpoint
		SET last_finality_synced_block_timestamp = GREATEST(last_finality_synced_block_timestamp, $1), last_finality_synced_at = now(), updated_at = now()
		WHERE id = 1
	`, maxSeen)
	return err
}

func handleFinalizedEvent(ctx context.Context, pool *store.Pool, ev tronclient.Event) error {
	var orderID int64
	err := pool.QueryRow(ctx, `
		UPDATE incoming_transactions
		SET confirmed = true
		WHERE tx_hash = $1 AND log_index = $2 AND confirmed = false
		RETURNING order_id
	`, ev.TransactionID, ev.EventIndex).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // not one of ours, or already marked confirmed
	}
	if err != nil {
		return err
	}

	if err := order.EvaluateFinal(ctx, pool, orderID); err != nil && !errors.Is(err, order.ErrTransitionNotDue) {
		return err
	}
	return nil
}
