package order

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coollazy/UCollection/internal/store"
)

// EvaluateConfirming checks whether the address has received enough
// (not-yet-necessarily-final) on-chain amount to move PENDING -> CONFIRMING
// (技術架構設計第5節「狀態轉換判斷條件」：只看下界，不卡上界). Called by internal/scanner
// task 2 after writing a new incoming_transactions row for this order.
// Returns ErrTransitionNotDue (not an error worth logging) if the order
// isn't PENDING or the sum hasn't reached amount_lower_bound yet.
func EvaluateConfirming(ctx context.Context, pool *store.Pool, orderID int64) error {
	return transition(ctx, pool, orderID, []Status{StatusPending}, func(ctx context.Context, tx pgx.Tx, current Order) (Status, bool, map[string]any, error) {
		var detected int64
		err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM incoming_transactions WHERE order_id = $1`, current.ID).Scan(&detected)
		if err != nil {
			return "", false, nil, err
		}
		snapshot := map[string]any{
			"detected_amount":    detected,
			"amount_lower_bound": current.AmountLowerBound,
		}
		if detected < current.AmountLowerBound {
			return "", false, snapshot, nil
		}
		return StatusConfirming, true, snapshot, nil
	}, "system", "偵測到入帳金額達下界")
}

// EvaluateFinal checks the solidified (only_confirmed=true) amount and
// moves CONFIRMING -> COMPLETED (in range) or OVERPAID (above range). Called
// by internal/scanner task 3 after marking an incoming_transactions row
// confirmed=true.
func EvaluateFinal(ctx context.Context, pool *store.Pool, orderID int64) error {
	return transition(ctx, pool, orderID, []Status{StatusConfirming}, func(ctx context.Context, tx pgx.Tx, current Order) (Status, bool, map[string]any, error) {
		confirmed, err := sumConfirmedAmount(ctx, tx, current.ID)
		if err != nil {
			return "", false, nil, err
		}
		snapshot := map[string]any{
			"confirmed_amount":   confirmed,
			"amount_lower_bound": current.AmountLowerBound,
			"amount_upper_bound": current.AmountUpperBound,
		}
		switch {
		case confirmed < current.AmountLowerBound:
			return "", false, snapshot, nil
		case confirmed > current.AmountUpperBound:
			return StatusOverpaid, true, snapshot, nil
		default:
			return StatusCompleted, true, snapshot, nil
		}
	}, "system", "鏈上最終確定金額判定")
}

// ExpireIfDue moves PENDING -> EXPIRED once expires_at has passed. The
// caller (internal/scanner task 1) is responsible for only calling this
// once its own checkpoint has caught up to expires_at — this function has
// no knowledge of TronGrid/checkpoints, it only compares timestamps (技術架構
// 設計第4節「任務1」守門條件屬scanner職責，非order職責).
func ExpireIfDue(ctx context.Context, pool *store.Pool, orderID int64) error {
	return transition(ctx, pool, orderID, []Status{StatusPending}, func(_ context.Context, _ pgx.Tx, current Order) (Status, bool, map[string]any, error) {
		snapshot := map[string]any{"expires_at": current.ExpiresAt}
		if !time.Now().After(current.ExpiresAt) {
			return "", false, snapshot, nil
		}
		return StatusExpired, true, snapshot, nil
	}, "system", "訂單有效期到期，累計金額仍未達下界")
}

// MarkStalled moves CONFIRMING -> CONFIRMATION_STALLED once
// confirming_at + confirmation_stall_timeout_seconds has passed without
// reaching solidified state.
func MarkStalled(ctx context.Context, pool *store.Pool, orderID int64) error {
	return transition(ctx, pool, orderID, []Status{StatusConfirming}, func(_ context.Context, _ pgx.Tx, current Order) (Status, bool, map[string]any, error) {
		if current.ConfirmingAt == nil {
			return "", false, nil, fmt.Errorf("order %d is CONFIRMING but confirming_at is null", current.ID)
		}
		deadline := current.ConfirmingAt.Add(time.Duration(current.ConfirmationStallTimeoutSeconds) * time.Second)
		snapshot := map[string]any{"confirming_at": *current.ConfirmingAt, "deadline": deadline}
		if !time.Now().After(deadline) {
			return "", false, snapshot, nil
		}
		return StatusConfirmationStalled, true, snapshot, nil
	}, "system", "確認等待逾時仍未達最終確定")
}
