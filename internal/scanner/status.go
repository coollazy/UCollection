package scanner

import (
	"context"
	"log"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// logCheckpointAnomaly records a checkpoint-regression attempt to audit_logs
// (技術架構設計第4節「儀表板可見度」：「同時記錄checkpoint異常倒退事件的稽核軌跡，供客訴時
// 追查」). The UPDATE itself already uses GREATEST() so a regression never
// actually moves the stored value backward — this only makes sure the
// attempt itself isn't silently swallowed. Audit failures never block the
// scan cycle (見既有慣例，如internal/consolidation/prepare.go).
func logCheckpointAnomaly(ctx context.Context, pool *store.Pool, column string, current, attempted int64) {
	err := audit.Log(ctx, pool, "system", "SCAN_CHECKPOINT_ANOMALY", nil, nil, map[string]any{
		"column":    column,
		"current":   current,
		"attempted": attempted,
	})
	if err != nil {
		log.Printf("scanner: log checkpoint anomaly: %v", err)
	}
}

// logLateFinalityConfirmation records a confirmed on-chain event that
// arrived after its order had already left CONFIRMING (e.g. a second
// payment confirms after the first already finalized the order as
// COMPLETED) — 階段06整合試跑情境E發現：這種事件過去被order.EvaluateFinal的白名單
// 靜默拒絕，只寫進app log，商戶端完全看不到訂單真實收到的金額已經跟系統記錄的不一致。
// Reuses the ILLEGAL_STATE_TRANSITION audit action internal/admin's manual
// override path already writes (見orders_override.go) so the existing
// /admin/audit-logs page surfaces this without any filter changes — actor
// is "system" here instead of "admin" to distinguish the two sources.
//
// This only makes the discrepancy visible; it deliberately does not change
// the order's status (CLAUDE.md業務鐵律5：系統不主動判斷改判方向，一律由商戶人工查證後
// 決定 — 且COMPLETED目前不在internal/order.manualWhitelist的允許來源狀態內，是否要
// 開放屬於獨立、更大的決策，不在這次修正範圍).
func logLateFinalityConfirmation(ctx context.Context, pool *store.Pool, orderID int64, ev tronclient.Event, currentStatus string) {
	var confirmedTotal int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM incoming_transactions WHERE order_id = $1 AND confirmed = true
	`, orderID).Scan(&confirmedTotal); err != nil {
		log.Printf("scanner: log late finality confirmation: sum confirmed for order %d: %v", orderID, err)
	}

	targetType := "order"
	err := audit.Log(ctx, pool, "system", "ILLEGAL_STATE_TRANSITION", &targetType, &orderID, map[string]any{
		"reason":              "late_finality_confirmation_after_terminal",
		"tx_hash":             ev.TransactionID,
		"event_amount":        ev.Value,
		"order_status":        currentStatus,
		"confirmed_total_now": confirmedTotal,
	})
	if err != nil {
		log.Printf("scanner: log late finality confirmation: %v", err)
	}
}

// SyncStatus is the "上次成功同步區塊" dashboard block (技術架構設計第11節「儀表板」
// 沿用第4節「儀表板可見度」).
type SyncStatus struct {
	LastSyncedAt         *time.Time
	LastFinalitySyncedAt *time.Time
}

// GetSyncStatus reads the two heartbeat timestamps straight off
// scan_checkpoint.
func GetSyncStatus(ctx context.Context, pool *store.Pool) (SyncStatus, error) {
	var s SyncStatus
	err := pool.QueryRow(ctx, `
		SELECT last_synced_at, last_finality_synced_at FROM scan_checkpoint WHERE id = 1
	`).Scan(&s.LastSyncedAt, &s.LastFinalitySyncedAt)
	return s, err
}

// StuckCount returns how many orders are past their wall-clock deadline
// (would-be EXPIRED/CONFIRMATION_STALLED) but not yet past task_lifecycle's
// gating watermark (last_synced_at/last_finality_synced_at) — i.e. orders
// "卡在守門條件、尚未能判定EXPIRED/STALLED" (技術架構設計第4節「儀表板可見度」). This
// mirrors sweepExpirable/sweepStallable's WHERE clauses (task_lifecycle.go)
// with the syncedUpTo comparison inverted.
func StuckCount(ctx context.Context, pool *store.Pool) (int, error) {
	var lastSyncedAt, lastFinalitySyncedAt *time.Time
	err := pool.QueryRow(ctx, `SELECT last_synced_at, last_finality_synced_at FROM scan_checkpoint WHERE id = 1`).
		Scan(&lastSyncedAt, &lastFinalitySyncedAt)
	if err != nil {
		return 0, err
	}

	var count int
	err = pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM orders
			 WHERE status = 'PENDING' AND expires_at < now() AND $1::timestamptz IS NOT NULL AND expires_at >= $1)
			+
			(SELECT COUNT(*) FROM orders
			 WHERE status = 'CONFIRMING'
			   AND confirming_at + (confirmation_stall_timeout_seconds || ' seconds')::interval < now()
			   AND $2::timestamptz IS NOT NULL
			   AND confirming_at + (confirmation_stall_timeout_seconds || ' seconds')::interval >= $2
			)
	`, lastSyncedAt, lastFinalitySyncedAt).Scan(&count)
	return count, err
}
