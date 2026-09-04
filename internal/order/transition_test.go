package order

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/store"
)

func TestEvaluateConfirming(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)
	ctx := context.Background()

	params := defaultParams("confirming-1", walletID)
	params.TargetAmount = 100
	params.AmountTolerancePercent = 0 // lower=upper=100
	o, err := CreateOrder(ctx, pool, params)
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	// 未達下界：不觸發
	insertTx(t, pool, o.ID, "tx1", 0, 99, false)
	if err := EvaluateConfirming(ctx, pool, o.ID); !errors.Is(err, ErrTransitionNotDue) {
		t.Fatalf("EvaluateConfirming() before threshold error = %v, want ErrTransitionNotDue", err)
	}
	if got := loadStatus(t, pool, o.ID); got != StatusPending {
		t.Fatalf("status after not-due evaluate = %s, want PENDING", got)
	}

	// 剛好達下界：觸發，且不論confirmed與否都要算入
	insertTx(t, pool, o.ID, "tx2", 0, 1, false)
	if err := EvaluateConfirming(ctx, pool, o.ID); err != nil {
		t.Fatalf("EvaluateConfirming() at threshold error = %v", err)
	}
	if got := loadStatus(t, pool, o.ID); got != StatusConfirming {
		t.Fatalf("status after evaluate = %s, want CONFIRMING", got)
	}
	if got := loadConfirmingAt(t, pool, o.ID); got == nil {
		t.Fatal("confirming_at not set after PENDING->CONFIRMING")
	}
}

func TestEvaluateFinal(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	t.Run("completed at exact bound", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		params := defaultParams("final-completed", wid)
		params.TargetAmount = 100
		params.AmountTolerancePercent = 0
		o, err := CreateOrder(ctx, pool, params)
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		forceStatus(t, pool, o.ID, StatusConfirming)
		insertTx(t, pool, o.ID, "tx1", 0, 100, true)

		if err := EvaluateFinal(ctx, pool, o.ID); err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if got := loadStatus(t, pool, o.ID); got != StatusCompleted {
			t.Fatalf("status = %s, want COMPLETED", got)
		}
	})

	t.Run("overpaid above upper bound", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		params := defaultParams("final-overpaid", wid)
		params.TargetAmount = 100
		params.AmountTolerancePercent = 0
		o, err := CreateOrder(ctx, pool, params)
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		forceStatus(t, pool, o.ID, StatusConfirming)
		insertTx(t, pool, o.ID, "tx1", 0, 150, true)

		if err := EvaluateFinal(ctx, pool, o.ID); err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if got := loadStatus(t, pool, o.ID); got != StatusOverpaid {
			t.Fatalf("status = %s, want OVERPAID", got)
		}
	})

	t.Run("not due below lower bound", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		params := defaultParams("final-notdue", wid)
		params.TargetAmount = 100
		params.AmountTolerancePercent = 0
		o, err := CreateOrder(ctx, pool, params)
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		forceStatus(t, pool, o.ID, StatusConfirming)
		insertTx(t, pool, o.ID, "tx1", 0, 50, true)

		if err := EvaluateFinal(ctx, pool, o.ID); !errors.Is(err, ErrTransitionNotDue) {
			t.Fatalf("EvaluateFinal() error = %v, want ErrTransitionNotDue", err)
		}
		if got := loadStatus(t, pool, o.ID); got != StatusConfirming {
			t.Fatalf("status = %s, want still CONFIRMING", got)
		}
	})
}

func TestExpireIfDue(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)
	ctx := context.Background()

	o, err := CreateOrder(ctx, pool, defaultParams("expire-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	if err := ExpireIfDue(ctx, pool, o.ID); !errors.Is(err, ErrTransitionNotDue) {
		t.Fatalf("ExpireIfDue() before expiry error = %v, want ErrTransitionNotDue", err)
	}

	setExpiresAt(t, pool, o.ID, time.Now().Add(-1*time.Minute))
	if err := ExpireIfDue(ctx, pool, o.ID); err != nil {
		t.Fatalf("ExpireIfDue() after expiry error = %v", err)
	}
	if got := loadStatus(t, pool, o.ID); got != StatusExpired {
		t.Fatalf("status = %s, want EXPIRED", got)
	}
}

func TestMarkStalled(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)
	ctx := context.Background()

	params := defaultParams("stall-1", walletID)
	params.ConfirmationStallTimeoutSeconds = 60
	o, err := CreateOrder(ctx, pool, params)
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	setConfirming(t, pool, o.ID, time.Now().Add(-30*time.Second))

	if err := MarkStalled(ctx, pool, o.ID); !errors.Is(err, ErrTransitionNotDue) {
		t.Fatalf("MarkStalled() before deadline error = %v, want ErrTransitionNotDue", err)
	}

	setConfirming(t, pool, o.ID, time.Now().Add(-90*time.Second))
	if err := MarkStalled(ctx, pool, o.ID); err != nil {
		t.Fatalf("MarkStalled() after deadline error = %v", err)
	}
	if got := loadStatus(t, pool, o.ID); got != StatusConfirmationStalled {
		t.Fatalf("status = %s, want CONFIRMATION_STALLED", got)
	}
}

func TestManualTransition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	t.Run("allowed: stalled to expired", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		o, err := CreateOrder(ctx, pool, defaultParams("manual-1", wid))
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		forceStatus(t, pool, o.ID, StatusConfirmationStalled)

		if err := ManualTransition(ctx, pool, o.ID, StatusExpired, "admin", "查證後確認未收到款項"); err != nil {
			t.Fatalf("ManualTransition() error = %v", err)
		}
		if got := loadStatus(t, pool, o.ID); got != StatusExpired {
			t.Fatalf("status = %s, want EXPIRED", got)
		}
		if by := loadLastChangedBy(t, pool, o.ID); by != "admin" {
			t.Errorf("changed_by = %q, want admin", by)
		}
	})

	t.Run("rejected: not in whitelist regardless of current status", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		o, err := CreateOrder(ctx, pool, defaultParams("manual-2", wid))
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		// CONFIRMING is never a valid manual-override target for any source
		// status (需求書5.2白名單只有COMPLETED/OVERPAID/EXPIRED三個目標).
		if err := ManualTransition(ctx, pool, o.ID, StatusConfirming, "admin", "x"); !errors.Is(err, ErrManualTransitionNotAllowed) {
			t.Fatalf("ManualTransition() error = %v, want ErrManualTransitionNotAllowed", err)
		}
	})

	t.Run("rejected: whitelisted target but wrong current status", func(t *testing.T) {
		resetDB(t, pool)
		wid := newMasterWallet(t, pool, testXpub)
		o, err := CreateOrder(ctx, pool, defaultParams("manual-3", wid))
		if err != nil {
			t.Fatalf("CreateOrder() error = %v", err)
		}
		// order is PENDING; EXPIRED->COMPLETED is whitelisted but this order never reached EXPIRED
		if err := ManualTransition(ctx, pool, o.ID, StatusCompleted, "admin", "x"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("ManualTransition() error = %v, want ErrInvalidTransition", err)
		}
	})
}

// TestManualTransition_ConcurrentOnlyOneWins exercises the SELECT ... FOR
// UPDATE serialization directly (技術架構設計第5節「原子性做法」): two goroutines
// race to manually override the same CONFIRMATION_STALLED order to two
// different terminal states. Exactly one must win.
func TestManualTransition_ConcurrentOnlyOneWins(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)
	ctx := context.Background()

	o, err := CreateOrder(ctx, pool, defaultParams("manual-race", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	forceStatus(t, pool, o.ID, StatusConfirmationStalled)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []Status{StatusCompleted, StatusOverpaid}
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ManualTransition(ctx, pool, o.ID, targets[i], "admin", "race")
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}

	final := loadStatus(t, pool, o.ID)
	if final != StatusCompleted && final != StatusOverpaid {
		t.Fatalf("final status = %s, want COMPLETED or OVERPAID", final)
	}
}

// --- test helpers operating directly on the DB, bypassing this package's
// own transactional functions where a test needs to force preconditions
// that only scanner (not yet built) would normally create. ---

func insertTx(t *testing.T, pool *store.Pool, orderID int64, txHash string, logIndex int, amount int64, confirmed bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, $2, $3, $4, $5, 1)
	`, orderID, txHash, logIndex, amount, confirmed)
	if err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}
}

func forceStatus(t *testing.T, pool *store.Pool, orderID int64, status Status) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE orders SET status = $1 WHERE id = $2`, status, orderID)
	if err != nil {
		t.Fatalf("force status: %v", err)
	}
}

func setExpiresAt(t *testing.T, pool *store.Pool, orderID int64, at time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE orders SET expires_at = $1 WHERE id = $2`, at, orderID)
	if err != nil {
		t.Fatalf("set expires_at: %v", err)
	}
}

func setConfirming(t *testing.T, pool *store.Pool, orderID int64, confirmingAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE orders SET status = $1, confirming_at = $2 WHERE id = $3`, StatusConfirming, confirmingAt, orderID)
	if err != nil {
		t.Fatalf("set confirming: %v", err)
	}
}

func loadStatus(t *testing.T, pool *store.Pool, orderID int64) Status {
	t.Helper()
	var s Status
	if err := pool.QueryRow(context.Background(), `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&s); err != nil {
		t.Fatalf("load status: %v", err)
	}
	return s
}

func loadConfirmingAt(t *testing.T, pool *store.Pool, orderID int64) *time.Time {
	t.Helper()
	var v *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT confirming_at FROM orders WHERE id = $1`, orderID).Scan(&v); err != nil {
		t.Fatalf("load confirming_at: %v", err)
	}
	return v
}

func loadLastChangedBy(t *testing.T, pool *store.Pool, orderID int64) string {
	t.Helper()
	var v string
	err := pool.QueryRow(context.Background(), `
		SELECT changed_by FROM order_state_transitions WHERE order_id = $1 ORDER BY id DESC LIMIT 1
	`, orderID).Scan(&v)
	if err != nil {
		t.Fatalf("load changed_by: %v", err)
	}
	return v
}
