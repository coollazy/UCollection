package scanner

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

const (
	testXpub           = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"
	testContract       = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	arbitrarySenderHex = "1111111111111111111111111111111111111111" // 20 bytes, sender doesn't matter for these tests
)

func testPool(t *testing.T) *store.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping test that needs a real PostgreSQL instance")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetDB(t *testing.T, pool *store.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions, webhook_deliveries, scan_checkpoint, audit_logs RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE system_params SET webhook_url = NULL, webhook_secret = NULL WHERE id = 1`); err != nil {
		t.Fatalf("reset system_params: %v", err)
	}
}

func newMasterWallet(t *testing.T, pool *store.Pool, xpub string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO master_wallets (xpub) VALUES ($1) RETURNING id`, xpub).Scan(&id)
	if err != nil {
		t.Fatalf("insert master_wallets: %v", err)
	}
	return id
}

func seedCheckpoint(t *testing.T, pool *store.Pool, syncedMs, finalityMs int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO scan_checkpoint (id, last_synced_block_timestamp, last_finality_synced_block_timestamp)
		VALUES (1, $1, $2)
	`, syncedMs, finalityMs)
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
}

func setCheckpointHeartbeats(t *testing.T, pool *store.Pool, lastSyncedAt, lastFinalitySyncedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE scan_checkpoint SET last_synced_at = $1, last_finality_synced_at = $2 WHERE id = 1
	`, lastSyncedAt, lastFinalitySyncedAt)
	if err != nil {
		t.Fatalf("set checkpoint heartbeats: %v", err)
	}
}

func forceStatus(t *testing.T, pool *store.Pool, orderID int64, status order.Status) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE orders SET status = $1 WHERE id = $2`, status, orderID)
	if err != nil {
		t.Fatalf("force status: %v", err)
	}
}

func insertUnconfirmedTx(t *testing.T, pool *store.Pool, orderID int64, txHash string, logIndex int, amount int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, $2, $3, $4, false, 1)
	`, orderID, txHash, logIndex, amount)
	if err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}
}

func loadOrderStatus(t *testing.T, pool *store.Pool, orderID int64) order.Status {
	t.Helper()
	var s order.Status
	if err := pool.QueryRow(context.Background(), `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&s); err != nil {
		t.Fatalf("load status: %v", err)
	}
	return s
}

func countIncomingTx(t *testing.T, pool *store.Pool, orderID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM incoming_transactions WHERE order_id = $1`, orderID).Scan(&n); err != nil {
		t.Fatalf("count incoming_transactions: %v", err)
	}
	return n
}

// toHexAddress converts a Tron Base58 address back to its raw 20-byte hex
// form, matching what TronGrid's contract-events endpoint returns for
// result.to/result.from — needed to build fixtures that will match a real
// order.Address produced by hdwallet.DeriveAddress.
func toHexAddress(t *testing.T, base58Address string) string {
	t.Helper()
	raw, _, err := base58.CheckDecode(base58Address)
	if err != nil {
		t.Fatalf("base58.CheckDecode(%q): %v", base58Address, err)
	}
	return hex.EncodeToString(raw)
}

type mockEvent struct {
	TxID           string
	EventIndex     int
	BlockNumber    int64
	BlockTimestamp int64
	FromHex        string
	ToHex          string
	Value          string
}

func eventsResponseJSON(events []mockEvent, fingerprint string) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i, e := range events {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"transaction_id":%q,"event_index":%d,"block_number":%d,"block_timestamp":%d,"result":{"from":%q,"to":%q,"value":%q}}`,
			e.TxID, e.EventIndex, e.BlockNumber, e.BlockTimestamp, e.FromHex, e.ToHex, e.Value)
	}
	b.WriteString(`],"success":true,"meta":{`)
	if fingerprint != "" {
		fmt.Fprintf(&b, `"fingerprint":%q`, fingerprint)
	}
	b.WriteString(`}}`)
	return b.String()
}

func mockTronGridOnce(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testOrder(t *testing.T, ctx context.Context, pool *store.Pool, merchantOrderNo string, walletID, target int64) order.Order {
	t.Helper()
	o, err := order.CreateOrder(ctx, pool, order.CreateParams{
		MerchantOrderNo:                 merchantOrderNo,
		MasterWalletID:                  walletID,
		TargetAmount:                    target,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          0,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	return o
}

func TestScanEventsOnce_MatchesOrderAndAdvancesCheckpoint(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "scan-1", walletID, 100)

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs-120_000, nowMs-120_000)

	toHex := toHexAddress(t, o.Address)
	eventTs := nowMs - 10_000
	srv := mockTronGridOnce(t, eventsResponseJSON([]mockEvent{
		{TxID: "tx-abc", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: eventTs, FromHex: arbitrarySenderHex, ToHex: toHex, Value: "100"},
	}, ""))

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanEventsOnce(ctx, deps); err != nil {
		t.Fatalf("scanEventsOnce() error = %v", err)
	}

	if n := countIncomingTx(t, pool, o.ID); n != 1 {
		t.Fatalf("incoming_transactions count = %d, want 1", n)
	}
	// target=100, tolerance=0 -> lower=upper=100, the single 100-amount tx meets it exactly
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusConfirming {
		t.Fatalf("order status = %s, want CONFIRMING", got)
	}

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&checkpoint); err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	wantCheckpoint := eventTs - checkpointSafetyBufferMs
	if checkpoint != wantCheckpoint {
		t.Errorf("checkpoint = %d, want %d", checkpoint, wantCheckpoint)
	}
}

func TestScanEventsOnce_DuplicateEventNotDoubleProcessed(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "scan-dup", walletID, 100)

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs-120_000, nowMs-120_000)
	toHex := toHexAddress(t, o.Address)
	body := eventsResponseJSON([]mockEvent{
		{TxID: "tx-dup", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: nowMs - 10_000, FromHex: arbitrarySenderHex, ToHex: toHex, Value: "100"},
	}, "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanEventsOnce(ctx, deps); err != nil {
		t.Fatalf("first scanEventsOnce() error = %v", err)
	}
	if err := scanEventsOnce(ctx, deps); err != nil {
		t.Fatalf("second scanEventsOnce() (overlapping window) error = %v", err)
	}

	if n := countIncomingTx(t, pool, o.ID); n != 1 {
		t.Fatalf("incoming_transactions count = %d, want 1 (dedup on tx_hash+log_index)", n)
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusConfirming {
		t.Fatalf("order status = %s, want CONFIRMING", got)
	}
}

func TestScanEventsOnce_NoEventsCheckpointDoesNotRegress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs-30_000, nowMs-30_000)

	srv := mockTronGridOnce(t, eventsResponseJSON(nil, ""))
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanEventsOnce(ctx, deps); err != nil {
		t.Fatalf("scanEventsOnce() error = %v", err)
	}

	var checkpoint int64
	if err := pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&checkpoint); err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint != nowMs-30_000 {
		t.Errorf("checkpoint = %d, want unchanged %d (GREATEST must not regress it)", checkpoint, nowMs-30_000)
	}
}

func TestScanFinalityOnce_MarksConfirmedAndCompletesOrder(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "final-1", walletID, 100)
	forceStatus(t, pool, o.ID, order.StatusConfirming)
	insertUnconfirmedTx(t, pool, o.ID, "tx-final", 0, 100)

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs-120_000, nowMs-120_000)
	toHex := toHexAddress(t, o.Address)
	srv := mockTronGridOnce(t, eventsResponseJSON([]mockEvent{
		{TxID: "tx-final", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: nowMs - 10_000, FromHex: arbitrarySenderHex, ToHex: toHex, Value: "100"},
	}, ""))

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanFinalityOnce(ctx, deps); err != nil {
		t.Fatalf("scanFinalityOnce() error = %v", err)
	}

	var confirmed bool
	if err := pool.QueryRow(ctx, `SELECT confirmed FROM incoming_transactions WHERE order_id = $1`, o.ID).Scan(&confirmed); err != nil {
		t.Fatalf("load confirmed: %v", err)
	}
	if !confirmed {
		t.Error("incoming_transactions.confirmed = false, want true")
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusCompleted {
		t.Fatalf("order status = %s, want COMPLETED", got)
	}
}

// TestScanFinalityOnce_LateConfirmationAfterTerminalLogsAudit covers 階段06
// 整合試跑情境E: an order already COMPLETED (an earlier payment already
// finalized it) receives a second confirmed event. order.EvaluateFinal
// correctly rejects it (ErrInvalidTransition — order is no longer
// CONFIRMING), and this must not be silently swallowed: the mark still
// lands, the order status is left untouched, and an ILLEGAL_STATE_TRANSITION
// audit entry records the discrepancy for the merchant to review.
func TestScanFinalityOnce_LateConfirmationAfterTerminalLogsAudit(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "final-late", walletID, 100)
	forceStatus(t, pool, o.ID, order.StatusCompleted)
	insertUnconfirmedTx(t, pool, o.ID, "tx-late-extra", 0, 20)

	nowMs := time.Now().UnixMilli()
	seedCheckpoint(t, pool, nowMs-120_000, nowMs-120_000)
	toHex := toHexAddress(t, o.Address)
	srv := mockTronGridOnce(t, eventsResponseJSON([]mockEvent{
		{TxID: "tx-late-extra", EventIndex: 0, BlockNumber: 1000, BlockTimestamp: nowMs - 10_000, FromHex: arbitrarySenderHex, ToHex: toHex, Value: "20"},
	}, ""))

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}
	if err := scanFinalityOnce(ctx, deps); err != nil {
		t.Fatalf("scanFinalityOnce() error = %v, want nil (late confirmation must not fail the scan cycle)", err)
	}

	var confirmed bool
	if err := pool.QueryRow(ctx, `SELECT confirmed FROM incoming_transactions WHERE order_id = $1 AND tx_hash = $2`, o.ID, "tx-late-extra").Scan(&confirmed); err != nil {
		t.Fatalf("load confirmed: %v", err)
	}
	if !confirmed {
		t.Error("incoming_transactions.confirmed = false, want true (mark still happens even though evaluation is rejected)")
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusCompleted {
		t.Fatalf("order status = %s, want unchanged COMPLETED (must not auto-correct a terminal order)", got)
	}

	var actionType, actor string
	var targetID int64
	err := pool.QueryRow(ctx, `
		SELECT action_type, actor, target_id FROM audit_logs
		WHERE action_type = 'ILLEGAL_STATE_TRANSITION' AND target_id = $1
	`, o.ID).Scan(&actionType, &actor, &targetID)
	if err != nil {
		t.Fatalf("expected an ILLEGAL_STATE_TRANSITION audit entry for order %d, query error: %v", o.ID, err)
	}
	if actor != "system" {
		t.Errorf("audit actor = %q, want %q", actor, "system")
	}
}

func TestSweepLifecycleOnce_ExpiryGatedByCheckpoint(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "expire-gate", walletID, 100)

	past := time.Now().Add(-1 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE orders SET expires_at = $1 WHERE id = $2`, past, o.ID); err != nil {
		t.Fatalf("set expires_at: %v", err)
	}
	seedCheckpoint(t, pool, 0, 0)

	// checkpoint heartbeat still behind expires_at -> must not expire yet
	setCheckpointHeartbeats(t, pool, past.Add(-1*time.Minute), past.Add(-1*time.Minute))
	if err := sweepLifecycleOnce(ctx, pool); err != nil {
		t.Fatalf("sweepLifecycleOnce() error = %v", err)
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusPending {
		t.Fatalf("status = %s, want still PENDING (checkpoint hasn't caught up)", got)
	}

	// checkpoint heartbeat now past expires_at -> must expire
	setCheckpointHeartbeats(t, pool, past.Add(1*time.Minute), past.Add(1*time.Minute))
	if err := sweepLifecycleOnce(ctx, pool); err != nil {
		t.Fatalf("sweepLifecycleOnce() error = %v", err)
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusExpired {
		t.Fatalf("status = %s, want EXPIRED", got)
	}
}

func TestSweepLifecycleOnce_StallGatedByCheckpoint(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()
	walletID := newMasterWallet(t, pool, testXpub)
	o := testOrder(t, ctx, pool, "stall-gate", walletID, 100)

	confirmingAt := time.Now().Add(-1 * time.Hour)
	deadline := confirmingAt.Add(900 * time.Second)
	forceStatus(t, pool, o.ID, order.StatusConfirming)
	if _, err := pool.Exec(ctx, `UPDATE orders SET confirming_at = $1 WHERE id = $2`, confirmingAt, o.ID); err != nil {
		t.Fatalf("set confirming_at: %v", err)
	}
	seedCheckpoint(t, pool, 0, 0)

	setCheckpointHeartbeats(t, pool, deadline.Add(-1*time.Minute), deadline.Add(-1*time.Minute))
	if err := sweepLifecycleOnce(ctx, pool); err != nil {
		t.Fatalf("sweepLifecycleOnce() error = %v", err)
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusConfirming {
		t.Fatalf("status = %s, want still CONFIRMING (finality checkpoint hasn't caught up)", got)
	}

	setCheckpointHeartbeats(t, pool, deadline.Add(1*time.Minute), deadline.Add(1*time.Minute))
	if err := sweepLifecycleOnce(ctx, pool); err != nil {
		t.Fatalf("sweepLifecycleOnce() error = %v", err)
	}
	if got := loadOrderStatus(t, pool, o.ID); got != order.StatusConfirmationStalled {
		t.Fatalf("status = %s, want CONFIRMATION_STALLED", got)
	}
}

func TestEnsureCheckpoint(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	ctx := context.Background()

	if err := EnsureCheckpoint(ctx, pool); err != nil {
		t.Fatalf("EnsureCheckpoint() error = %v", err)
	}
	var first int64
	if err := pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&first); err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if first == 0 {
		t.Fatal("checkpoint seed is zero, want a real timestamp")
	}

	// calling again must not overwrite an already-seeded row
	if err := EnsureCheckpoint(ctx, pool); err != nil {
		t.Fatalf("EnsureCheckpoint() second call error = %v", err)
	}
	var second int64
	if err := pool.QueryRow(ctx, `SELECT last_synced_block_timestamp FROM scan_checkpoint WHERE id = 1`).Scan(&second); err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if second != first {
		t.Errorf("second EnsureCheckpoint() changed checkpoint from %d to %d, want unchanged (ON CONFLICT DO NOTHING)", first, second)
	}
}

func TestRun_HonorsContextCancellation(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(eventsResponseJSON(nil, "")))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(srv.URL, ""), ContractAddress: testContract}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, deps) }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() error = nil, want context.Canceled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return within 10s of context cancellation")
	}
}
