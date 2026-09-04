package webhook

import (
	"context"

	"github.com/coollazy/UCollection/internal/store"
)

// PendingSummary returns the two counts shown on the admin dashboard's
// passive-visibility block (技術架構設計第8節「被動可見度」：「失敗中/待重試」與
// 「待設定（awaiting_config）」兩類筆數).
func PendingSummary(ctx context.Context, pool *store.Pool) (failedOrRetrying, awaitingConfig int, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('pending', 'sending', 'failed')),
			COUNT(*) FILTER (WHERE status = 'awaiting_config')
		FROM webhook_deliveries
	`).Scan(&failedOrRetrying, &awaitingConfig)
	return failedOrRetrying, awaitingConfig, err
}
