package scanner

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

// runLifecycleTask is 任務1「訂單生命週期巡查」(技術架構設計第4節): pure DB
// queries, gated by 任務2/任務3 having actually caught up past the relevant
// timestamp before committing to EXPIRED/CONFIRMATION_STALLED — avoids
// misjudging a right-on-time payment as expired, or a system processing lag
// as a stalled order (見本模組plan notes：改用last_synced_at/
// last_finality_synced_at心跳時間戳實作這個守門條件).
func runLifecycleTask(ctx context.Context, deps Deps) error {
	for {
		if err := sweepLifecycleOnce(ctx, deps.Pool); err != nil {
			log.Printf("scanner: lifecycle task: sweep failed, will retry: %v", err)
		}
		if !sleepOrDone(ctx, lifecycleTickerPeriod) {
			return nil
		}
	}
}

func sweepLifecycleOnce(ctx context.Context, pool *store.Pool) error {
	var lastSyncedAt, lastFinalitySyncedAt *time.Time
	err := pool.QueryRow(ctx, `SELECT last_synced_at, last_finality_synced_at FROM scan_checkpoint WHERE id = 1`).Scan(&lastSyncedAt, &lastFinalitySyncedAt)
	if err != nil {
		return err
	}

	if lastSyncedAt != nil {
		if err := sweepExpirable(ctx, pool, *lastSyncedAt); err != nil {
			return err
		}
	}
	if lastFinalitySyncedAt != nil {
		if err := sweepStallable(ctx, pool, *lastFinalitySyncedAt); err != nil {
			return err
		}
	}
	return nil
}

func sweepExpirable(ctx context.Context, pool *store.Pool, syncedUpTo time.Time) error {
	rows, err := pool.Query(ctx, `
		SELECT id FROM orders WHERE status = 'PENDING' AND expires_at < now() AND expires_at < $1
	`, syncedUpTo)
	if err != nil {
		return err
	}
	ids, err := collectIDs(rows)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if err := order.ExpireIfDue(ctx, pool, id); err != nil && !errors.Is(err, order.ErrTransitionNotDue) {
			log.Printf("scanner: lifecycle task: expire order %d: %v", id, err)
		}
	}
	return nil
}

func sweepStallable(ctx context.Context, pool *store.Pool, syncedUpTo time.Time) error {
	rows, err := pool.Query(ctx, `
		SELECT id FROM orders
		WHERE status = 'CONFIRMING'
		  AND confirming_at + (confirmation_stall_timeout_seconds || ' seconds')::interval < now()
		  AND confirming_at + (confirmation_stall_timeout_seconds || ' seconds')::interval < $1
	`, syncedUpTo)
	if err != nil {
		return err
	}
	ids, err := collectIDs(rows)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if err := order.MarkStalled(ctx, pool, id); err != nil && !errors.Is(err, order.ErrTransitionNotDue) {
			log.Printf("scanner: lifecycle task: stall order %d: %v", id, err)
		}
	}
	return nil
}

func collectIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}) ([]int64, error) {
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
