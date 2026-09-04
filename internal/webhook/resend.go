package webhook

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coollazy/UCollection/internal/store"
)

// Resend implements 技術架構設計第8節「手動重發」: triggerable regardless of the
// delivery's current status, always writes a webhook_delivery_attempts row
// with triggered_by='manual', and never touches attempt_count/
// next_retry_at — those belong exclusively to the automatic retry
// schedule. On success the delivery becomes 'delivered'; on failure its
// status is left exactly as it was found.
//
// No HTTP route calls this yet (需求書5.5的手動重發按鈕屬admin模組，還沒做) —
// exported now so admin can call it directly once built, instead of this
// package needing another pass later.
func Resend(ctx context.Context, pool *store.Pool, httpClient *http.Client, deliveryID int64) error {
	if httpClient == nil {
		httpClient = newHTTPClient()
	}

	var payload string
	err := pool.QueryRow(ctx, `SELECT payload FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&payload)
	if err != nil {
		return fmt.Errorf("webhook: resend: load delivery %d: %w", deliveryID, err)
	}

	url, secret, configured, err := loadWebhookConfig(ctx, pool)
	if err != nil {
		return fmt.Errorf("webhook: resend: load config: %w", err)
	}
	if !configured {
		return fmt.Errorf("webhook: resend: webhook_url/webhook_secret not configured")
	}

	result, sendErr := sendWebhook(ctx, httpClient, url, secret, []byte(payload))
	success := sendErr == nil && result.HTTPStatus >= 200 && result.HTTPStatus < 300

	return recordAttemptAndAdvance(ctx, pool, deliveryID, "manual", result, sendErr, success)
}
