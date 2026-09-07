package order

import (
	"context"

	"github.com/coollazy/UCollection/internal/store"
)

// DashboardStats is the three-number summary shown on the admin dashboard
// (技術架構設計第11節「儀表板」).
type DashboardStats struct {
	// TotalCollected sums incoming_transactions.amount (confirmed=true) for
	// every COMPLETED/OVERPAID order, all-time (第11節：「不做期間篩選，商戶若需要
	// 期間數據，用CSV/Excel匯出篩選查看」).
	TotalCollected int64
	// PendingCount is PENDING+CONFIRMING orders — still inside normal
	// automated monitoring, not abnormal.
	PendingCount int
	// AbnormalCount is OVERPAID + CONFIRMATION_STALLED + (EXPIRED that
	// received at least one incoming_transactions row) + (COMPLETED that has
	// a system-flagged late finality confirmation after going terminal —
	// see internal/scanner/status.go's logLateFinalityConfirmation, 階段06
	// 情境E) — orders that need merchant review (第11節：判準是「是否需要商戶查證/
	// 處理」，不是「是否為終態」).
	AbnormalCount int
}

// GetDashboardStats computes the three summary numbers in three queries
// (kept separate rather than one mega-query — each is independently simple
// to read and none of them are performance-sensitive at this data volume).
func GetDashboardStats(ctx context.Context, pool *store.Pool) (DashboardStats, error) {
	var stats DashboardStats

	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(it.amount), 0)
		FROM incoming_transactions it
		JOIN orders o ON o.id = it.order_id
		WHERE it.confirmed = true AND o.status IN ($1, $2)
	`, StatusCompleted, StatusOverpaid).Scan(&stats.TotalCollected)
	if err != nil {
		return DashboardStats{}, err
	}

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM orders WHERE status IN ($1, $2)
	`, StatusPending, StatusConfirming).Scan(&stats.PendingCount)
	if err != nil {
		return DashboardStats{}, err
	}

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM orders o
		WHERE o.status IN ($1, $2)
		   OR (o.status = $3 AND EXISTS (SELECT 1 FROM incoming_transactions it WHERE it.order_id = o.id))
		   OR (o.status = $4 AND EXISTS (
		       SELECT 1 FROM audit_logs a
		       WHERE a.target_type = 'order' AND a.target_id = o.id
		         AND a.action_type = 'ILLEGAL_STATE_TRANSITION' AND a.actor = 'system'
		   ))
	`, StatusOverpaid, StatusConfirmationStalled, StatusExpired, StatusCompleted).Scan(&stats.AbnormalCount)
	if err != nil {
		return DashboardStats{}, err
	}

	return stats, nil
}
