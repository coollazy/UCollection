package webhook

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

// ErrWebhookNotConfigured is returned by SendTestPing when webhook_url/
// webhook_secret aren't both set yet — the caller (internal/admin) should
// show a "請先設定webhook" message instead of attempting a send.
var ErrWebhookNotConfigured = errors.New("webhook: url/secret not configured")

// testPingPayload is intentionally its own minimal shape, not a reuse of
// internal/order's unexported webhookPayload (order-specific fields like
// order_id/target_amount don't apply to a connectivity test — 技術架構設計第8節
// 「測試」only specifies event_type=TEST_PING, everything else here is just
// enough for the receiving endpoint to log something recognizable).
type testPingPayload struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"`
}

// SendTestPing implements 技術架構設計第8節「測試」／第11節 POST
// /admin/webhook-config/test: sends one synchronous TEST_PING request using
// whatever webhook_url/webhook_secret are currently configured. It never
// writes to webhook_deliveries and never consumes retry budget — this is a
// one-off connectivity check, not a real delivery. Signature mirrors
// Resend's (pool + httpClient directly, not a Deps struct) so admin can call
// it the same way.
func SendTestPing(ctx context.Context, pool *store.Pool, httpClient *http.Client) (httpStatus int, responseBody string, err error) {
	if httpClient == nil {
		httpClient = newHTTPClient()
	}

	url, secret, configured, err := loadWebhookConfig(ctx, pool)
	if err != nil {
		return 0, "", fmt.Errorf("webhook: test ping: load config: %w", err)
	}
	if !configured {
		return 0, "", ErrWebhookNotConfigured
	}

	eventID, err := randomEventID()
	if err != nil {
		return 0, "", fmt.Errorf("webhook: test ping: generate event_id: %w", err)
	}

	payload, err := json.Marshal(testPingPayload{
		EventID:    eventID,
		EventType:  "TEST_PING",
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return 0, "", fmt.Errorf("webhook: test ping: marshal payload: %w", err)
	}

	result, sendErr := sendWebhook(ctx, httpClient, url, secret, payload)
	if sendErr != nil {
		return 0, "", fmt.Errorf("webhook: test ping: send: %w", sendErr)
	}
	return result.HTTPStatus, result.ResponseBody, nil
}

// randomEventID mirrors internal/order's randomToken (32 bytes crypto/rand,
// base64.RawURLEncoding) — same established shape, duplicated locally since
// this project has no shared random-token helper package (see
// internal/order/order.go and internal/auth/session.go, each rolling their
// own copy of this exact pattern).
func randomEventID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
