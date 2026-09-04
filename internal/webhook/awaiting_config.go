package webhook

import (
	"context"
	"log"

	"github.com/coollazy/UCollection/internal/store"
)

func runAwaitingConfigTask(ctx context.Context, deps Deps) error {
	for {
		if err := sweepAwaitingConfig(ctx, deps.Pool); err != nil {
			log.Printf("webhook: awaiting_config task: sweep failed, will retry: %v", err)
		}
		if !sleepOrDone(ctx, awaitingConfigTickerPeriod) {
			return nil
		}
	}
}

// sweepAwaitingConfig implements 技術架構設計第8節「未設定URL/secret的邊界」：每小時
// 檢查一次是否已補齊webhook_url/webhook_secret，補齊後轉入pending開始正常倒數.
func sweepAwaitingConfig(ctx context.Context, pool *store.Pool) error {
	_, _, configured, err := loadWebhookConfig(ctx, pool)
	if err != nil {
		return err
	}
	if !configured {
		return nil
	}

	_, err = pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status = 'pending', next_retry_at = now(), updated_at = now()
		WHERE status = 'awaiting_config'
	`)
	return err
}
