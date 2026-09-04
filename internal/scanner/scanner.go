// Package scanner runs the on-chain event scanning background tasks. See
// docs/開發流程框架-03-技術架構設計.md 第4節.
package scanner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

const (
	contractEventName = "Transfer"
	pageLimit         = 200

	// checkpointSafetyBufferMs is the safety margin held back from the
	// newest event timestamp seen each poll before advancing
	// last_synced_block_timestamp. Equivalent to the 拍板值「N=20個區塊≈60秒」
	// (見技術架構設計第4節、變更紀錄v0.6) — expressed in milliseconds here
	// because the checkpoint itself is a timestamp watermark, not a block
	// count (see this module's plan notes for why).
	checkpointSafetyBufferMs = 60_000

	eventsTickerActive    = 5 * time.Second
	eventsTickerIdle      = 30 * time.Second
	finalityTickerPeriod  = 15 * time.Second
	lifecycleTickerPeriod = 30 * time.Second
)

// Deps are the dependencies Run needs. Callers (cmd/ucollection) build
// these from internal/config-sourced values.
type Deps struct {
	Pool            *store.Pool
	TronClient      *tronclient.Client
	ContractAddress string
}

// EnsureCheckpoint seeds the single scan_checkpoint row if it doesn't
// exist yet, watermarked to "now minus the safety buffer" — a fresh
// deployment has no historical orders to backfill, so there's no reason to
// scan from further back (呼應第4節「首次部署SOP」：必須先有一筆checkpoint才能開放
// 接單). It's safe to call repeatedly (ON CONFLICT DO NOTHING). Callers
// should call this synchronously before accepting any traffic that could
// create orders — not just as part of Run's background loop.
func EnsureCheckpoint(ctx context.Context, pool *store.Pool) error {
	seed := time.Now().UnixMilli() - checkpointSafetyBufferMs
	_, err := pool.Exec(ctx, `
		INSERT INTO scan_checkpoint (id, last_synced_block_timestamp, last_finality_synced_block_timestamp)
		VALUES (1, $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, seed)
	if err != nil {
		return fmt.Errorf("scanner: seed checkpoint: %w", err)
	}
	return nil
}

// Run starts the three always-on background tasks (技術架構設計第4節) and
// blocks until ctx is cancelled or one of them fails. A failure in one
// task cancels the other two.
func Run(ctx context.Context, deps Deps) error {
	if err := EnsureCheckpoint(ctx, deps.Pool); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 3)

	run := func(name string, task func(context.Context, Deps) error) {
		defer wg.Done()
		if err := task(ctx, deps); err != nil {
			log.Printf("scanner: task %s stopped: %v", name, err)
			errs <- fmt.Errorf("scanner: task %s: %w", name, err)
			cancel()
			return
		}
		errs <- nil
	}

	wg.Add(3)
	go run("events", runEventsTask)
	go run("finality", runFinalityTask)
	go run("lifecycle", runLifecycleTask)

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// sleepOrDone waits for d or ctx cancellation, whichever comes first.
// Returns false if ctx was cancelled (caller should stop looping).
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
