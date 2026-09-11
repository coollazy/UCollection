package checkout

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
)

// testXpub is a known-good xpub reused across this codebase's tests (see
// internal/consolidation/order test vectors) — used here only to create
// orders with valid derived addresses; no funds are ever touched.
const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

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
	_, err := pool.Exec(context.Background(),
		`TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions, webhook_deliveries, audit_logs RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

func newPendingOrder(t *testing.T, pool *store.Pool, merchantOrderNo string) order.Order {
	t.Helper()
	// Reuse a single master wallet across calls: a fresh wallet each time
	// would start at last_derived_index=0, so every "first" order under the
	// same testXpub derives the identical index-1 address and collides on
	// orders_address_key. One shared wallet hands out increasing indices
	// (1, 2, 3, ...) → distinct addresses.
	var walletID int64
	err := pool.QueryRow(context.Background(), `SELECT id FROM master_wallets ORDER BY id LIMIT 1`).Scan(&walletID)
	if err != nil {
		if ierr := pool.QueryRow(context.Background(), `INSERT INTO master_wallets (xpub) VALUES ($1) RETURNING id`, testXpub).Scan(&walletID); ierr != nil {
			t.Fatalf("insert master_wallets: %v", ierr)
		}
	}
	o, err := order.CreateOrder(context.Background(), pool, order.CreateParams{
		MerchantOrderNo:                 merchantOrderNo,
		MasterWalletID:                  walletID,
		TargetAmount:                    100_000000, // 100 USDT
		ValiditySeconds:                 900,
		AmountTolerancePercent:          1.5, // bounds: 98.5 / 101.5 USDT — must NOT leak to the page
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	return o
}

// setStatus forces an order into a given status directly, so tests can cover
// every display branch without driving the full state machine.
func setStatus(t *testing.T, pool *store.Pool, id int64, status order.Status) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status = $1 WHERE id = $2`, string(status), id); err != nil {
		t.Fatalf("set status %s: %v", status, err)
	}
}

func testMux(pool *store.Pool) *http.ServeMux {
	mux := http.NewServeMux()
	RegisterRoutes(mux, Deps{Pool: pool})
	return mux
}
