package webhook

import (
	"context"
	"log"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

func runWorkerTask(ctx context.Context, deps Deps) error {
	for {
		if err := processBatch(ctx, deps); err != nil {
			log.Printf("webhook: worker task: batch failed, will retry: %v", err)
		}
		if !sleepOrDone(ctx, workerTickerPeriod) {
			return nil
		}
	}
}

func processBatch(ctx context.Context, deps Deps) error {
	ids, err := pickBatch(ctx, deps.Pool, pickBatchLimit)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := processOne(ctx, deps, id); err != nil {
			log.Printf("webhook: worker task: process delivery %d: %v", id, err)
		}
	}
	return nil
}

// pickBatch implements 技術架構設計第8節「Worker取件與逾時回收」verbatim: pending
// deliveries whose next_retry_at is due, plus sending deliveries stuck past
// sendingTimeout (process died mid-send). Marking them 'sending' inside the
// same transaction before releasing the row lock is what lets multiple app
// instances share one queue without double-sending (see 第8節 wording).
func pickBatch(ctx context.Context, pool *store.Pool, limit int) ([]int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// sendingTimeout (2 minutes) is a compile-time constant, not passed as a
	// query parameter — Postgres interval literals don't parse Go's
	// Duration.String() format (e.g. "2m0s"), and there's no need to make
	// this runtime-configurable.
	rows, err := tx.Query(ctx, `
		SELECT id FROM webhook_deliveries
		WHERE (status = 'pending' AND next_retry_at <= now())
		   OR (status = 'sending' AND updated_at < now() - interval '2 minutes')
		ORDER BY next_retry_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for _, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET status = 'sending', updated_at = now() WHERE id = $1`, id); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

func processOne(ctx context.Context, deps Deps, deliveryID int64) error {
	var payload string
	err := deps.Pool.QueryRow(ctx, `SELECT payload FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&payload)
	if err != nil {
		return err
	}

	url, secret, configured, err := loadWebhookConfig(ctx, deps.Pool)
	if err != nil {
		return err
	}
	if !configured {
		// Shouldn't normally happen — a delivery only leaves awaiting_config
		// once both are set — but defensive in case config was cleared
		// again in between (e.g. secret rotated to empty by mistake).
		_, err := deps.Pool.Exec(ctx, `UPDATE webhook_deliveries SET status = 'awaiting_config', updated_at = now() WHERE id = $1`, deliveryID)
		return err
	}

	result, sendErr := sendWebhook(ctx, deps.HTTPClient, url, secret, []byte(payload))

	success := sendErr == nil && result.HTTPStatus >= 200 && result.HTTPStatus < 300
	return recordAttemptAndAdvance(ctx, deps.Pool, deliveryID, "auto", result, sendErr, success)
}

// loadWebhookConfig reads webhook_url/webhook_secret fresh — never cached
// — so rotations apply to the very next send (技術架構設計第8節).
func loadWebhookConfig(ctx context.Context, pool *store.Pool) (url, secret string, configured bool, err error) {
	var u, s *string
	err = pool.QueryRow(ctx, `SELECT webhook_url, webhook_secret FROM system_params WHERE id = 1`).Scan(&u, &s)
	if err != nil {
		return "", "", false, err
	}
	if u == nil || *u == "" || s == nil || *s == "" {
		return "", "", false, nil
	}
	return *u, *s, true, nil
}

// recordAttemptAndAdvance writes one webhook_delivery_attempts row and
// updates webhook_deliveries accordingly. For automatic attempts:
// success -> delivered; failure -> pending with exponential backoff, or
// failed once retryWindow has elapsed since the first automatic attempt
// (技術架構設計第8節「重試策略」). attempt_count only counts automatic attempts
// (manual resends never touch it, see resend.go).
func recordAttemptAndAdvance(ctx context.Context, pool *store.Pool, deliveryID int64, triggeredBy string, result sendResult, sendErr error, success bool) error {
	var httpStatus *int
	var responseBody *string
	if sendErr == nil {
		httpStatus = &result.HTTPStatus
		responseBody = &result.ResponseBody
	} else {
		msg := sendErr.Error()
		if len(msg) > maxResponseBodyBytes {
			msg = msg[:maxResponseBodyBytes]
		}
		responseBody = &msg
	}

	var attemptNo int
	err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(attempt_no), 0) + 1 FROM webhook_delivery_attempts WHERE delivery_id = $1`, deliveryID).Scan(&attemptNo)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO webhook_delivery_attempts (delivery_id, attempt_no, http_status, response_body, triggered_by)
		VALUES ($1, $2, $3, $4, $5)
	`, deliveryID, attemptNo, httpStatus, responseBody, triggeredBy); err != nil {
		return err
	}

	if triggeredBy == "manual" {
		return advanceManual(ctx, pool, deliveryID, success)
	}
	return advanceAuto(ctx, pool, deliveryID, success)
}

func advanceAuto(ctx context.Context, pool *store.Pool, deliveryID int64, success bool) error {
	if success {
		_, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET status = 'delivered', attempt_count = attempt_count + 1, updated_at = now() WHERE id = $1`, deliveryID)
		return err
	}

	var firstAttemptAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT MIN(attempted_at) FROM webhook_delivery_attempts WHERE delivery_id = $1 AND triggered_by = 'auto'
	`, deliveryID).Scan(&firstAttemptAt)
	if err != nil {
		return err
	}
	if firstAttemptAt != nil && time.Since(*firstAttemptAt) > retryWindow {
		_, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET status = 'failed', attempt_count = attempt_count + 1, updated_at = now() WHERE id = $1`, deliveryID)
		return err
	}

	var attemptCount int
	if err := pool.QueryRow(ctx, `SELECT attempt_count FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&attemptCount); err != nil {
		return err
	}
	newAttemptCount := attemptCount + 1
	delay := retryBaseDelay << (newAttemptCount - 1) //nolint:gosec // newAttemptCount is small and bounded by the 24h retryWindow in practice
	if delay > retryMaxDelay || delay <= 0 {
		delay = retryMaxDelay
	}
	nextRetryAt := time.Now().Add(delay)

	_, err = pool.Exec(ctx, `
		UPDATE webhook_deliveries SET status = 'pending', attempt_count = $1, next_retry_at = $2, updated_at = now() WHERE id = $3
	`, newAttemptCount, nextRetryAt, deliveryID)
	return err
}

func advanceManual(ctx context.Context, pool *store.Pool, deliveryID int64, success bool) error {
	if success {
		_, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET status = 'delivered', updated_at = now() WHERE id = $1`, deliveryID)
		return err
	}
	// 技術架構設計第8節「手動重發」：失敗→維持原status不變，不動attempt_count/
	// next_retry_at — nothing to do here.
	return nil
}
