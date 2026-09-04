package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestGetSyncStatus(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs, nowMs)
	syncedAt := time.Now().Add(-5 * time.Second)
	finalityAt := time.Now().Add(-10 * time.Second)
	setCheckpointHeartbeats(t, pool, syncedAt, finalityAt)

	got, err := GetSyncStatus(ctx, pool)
	if err != nil {
		t.Fatalf("GetSyncStatus() error = %v", err)
	}
	if got.LastSyncedAt == nil || !got.LastSyncedAt.Equal(syncedAt) {
		t.Errorf("LastSyncedAt = %v, want %v", got.LastSyncedAt, syncedAt)
	}
	if got.LastFinalitySyncedAt == nil || !got.LastFinalitySyncedAt.Equal(finalityAt) {
		t.Errorf("LastFinalitySyncedAt = %v, want %v", got.LastFinalitySyncedAt, finalityAt)
	}
}

// TestStuckCount_ZeroBeforeFirstHeartbeat mirrors sweepExpirable/
// sweepStallable's own "nil heartbeat means skip entirely" gate
// (task_lifecycle.go) — before the scanner has ever completed a cycle,
// StuckCount must report 0, not "every already-expired order".
func TestStuckCount_ZeroBeforeFirstHeartbeat(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "stuck-1", walletID, 100)
	// Force expires_at into the past without waiting.
	if _, err := pool.Exec(ctx, `UPDATE orders SET expires_at = now() - interval '1 hour' WHERE id = $1`, o.ID); err != nil {
		t.Fatalf("force expires_at: %v", err)
	}
	seedCheckpoint(t, pool, 0, 0) // last_synced_at/last_finality_synced_at stay NULL

	got, err := StuckCount(ctx, pool)
	if err != nil {
		t.Fatalf("StuckCount() error = %v", err)
	}
	if got != 0 {
		t.Errorf("StuckCount() = %d, want 0 (no heartbeat yet)", got)
	}
}

// TestStuckCount_CountsOrderPastDeadlineButNotYetSynced covers the actual
// "卡住" case: wall-clock deadline has passed, but the scanner's watermark
// hasn't caught up to it yet.
func TestStuckCount_CountsOrderPastDeadlineButNotYetSynced(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "stuck-2", walletID, 100)
	if _, err := pool.Exec(ctx, `UPDATE orders SET expires_at = now() - interval '1 minute' WHERE id = $1`, o.ID); err != nil {
		t.Fatalf("force expires_at: %v", err)
	}
	seedCheckpoint(t, pool, 0, 0)
	// Heartbeat is older than the order's expiry — scanner hasn't caught up.
	setCheckpointHeartbeats(t, pool, time.Now().Add(-2*time.Minute), time.Now().Add(-2*time.Minute))

	got, err := StuckCount(ctx, pool)
	if err != nil {
		t.Fatalf("StuckCount() error = %v", err)
	}
	if got != 1 {
		t.Errorf("StuckCount() = %d, want 1", got)
	}

	// Once the heartbeat catches up past expires_at, it's no longer "stuck"
	// (task_lifecycle would have swept it by now).
	setCheckpointHeartbeats(t, pool, time.Now(), time.Now())
	got, err = StuckCount(ctx, pool)
	if err != nil {
		t.Fatalf("StuckCount() error = %v", err)
	}
	if got != 0 {
		t.Errorf("StuckCount() after catch-up = %d, want 0", got)
	}
}

func TestScanEventsOnce_CheckpointRegressionIsAudited(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	// Seed a checkpoint already ahead of what the (empty) mock response
	// would compute, forcing newCheckpoint < checkpoint.
	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs, nowMs)

	srv := mockTronGridOnce(t, eventsResponseJSON(nil, ""))
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanEventsOnce(ctx, deps); err != nil {
		t.Fatalf("scanEventsOnce() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action_type = 'SCAN_CHECKPOINT_ANOMALY'`).Scan(&count); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("SCAN_CHECKPOINT_ANOMALY audit_logs count = %d, want 1", count)
	}

	// GREATEST() must still have protected the stored value itself.
	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&checkpoint); err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint != nowMs {
		t.Errorf("checkpoint = %d, want unchanged %d", checkpoint, nowMs)
	}
}
