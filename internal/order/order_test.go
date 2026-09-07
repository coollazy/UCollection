package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/coollazy/UCollection/internal/store"
)

// testXpub and its known index->address vectors are the same
// cross-validated test data used in internal/hdwallet/hdwallet_test.go (見
// 驗證結論-01-HD地址衍生.md：4套獨立實作交叉驗證過).
const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

const testAddressIndex1 = "TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK"

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

// resetDB truncates every table this package touches, so each test starts
// from a clean slate regardless of execution order.
func resetDB(t *testing.T, pool *store.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE master_wallets, orders, order_state_transitions, incoming_transactions, webhook_deliveries, audit_logs RESTART IDENTITY CASCADE`)
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

func defaultParams(merchantOrderNo string, masterWalletID int64) CreateParams {
	return CreateParams{
		MerchantOrderNo:                 merchantOrderNo,
		MasterWalletID:                  masterWalletID,
		TargetAmount:                    100_000000, // 100 USDT, 6位小數最小單位
		ValiditySeconds:                 900,
		AmountTolerancePercent:          1.5,
		ConfirmationStallTimeoutSeconds: 900,
	}
}

func TestCreateOrder(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	o, err := CreateOrder(context.Background(), pool, defaultParams("order-1", walletID))
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}

	// index 0 永遠保留：第一筆訂單必須拿到 index 1，位址與已知交叉驗證過的向量完全吻合。
	if o.DerivationIndex != 1 {
		t.Errorf("DerivationIndex = %d, want 1 (index 0 must be reserved)", o.DerivationIndex)
	}
	if o.Address != testAddressIndex1 {
		t.Errorf("Address = %q, want %q", o.Address, testAddressIndex1)
	}
	if o.Status != StatusPending {
		t.Errorf("Status = %q, want PENDING", o.Status)
	}
	// 1.5% of 100_000000 = 1_500000
	if o.AmountLowerBound != 98_500000 {
		t.Errorf("AmountLowerBound = %d, want 98500000", o.AmountLowerBound)
	}
	if o.AmountUpperBound != 101_500000 {
		t.Errorf("AmountUpperBound = %d, want 101500000", o.AmountUpperBound)
	}
	if o.PublicToken == "" {
		t.Error("PublicToken is empty")
	}
}

func TestCreateOrder_DuplicateMerchantOrderNo(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	if _, err := CreateOrder(context.Background(), pool, defaultParams("dup-order", walletID)); err != nil {
		t.Fatalf("first CreateOrder() error = %v", err)
	}
	_, err := CreateOrder(context.Background(), pool, defaultParams("dup-order", walletID))
	if !errors.Is(err, ErrDuplicateMerchantOrderNo) {
		t.Fatalf("second CreateOrder() error = %v, want ErrDuplicateMerchantOrderNo", err)
	}
}

func TestCreateOrder_RejectsNonPositiveAmount(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	params := defaultParams("bad-amount", walletID)
	params.TargetAmount = 0
	if _, err := CreateOrder(context.Background(), pool, params); err == nil {
		t.Fatal("CreateOrder() error = nil, want error for zero target amount")
	}
}

// TestCreateOrder_ConcurrentAllocationNoDuplicateIndex is the test the
// architecture doc explicitly calls for (第5節「驗收要求」): a real
// pg_advisory_xact_lock race, not a mock, since that's the only way to
// actually exercise the serialization.
func TestCreateOrder_ConcurrentAllocationNoDuplicateIndex(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool, testXpub)

	const n = 20
	var wg sync.WaitGroup
	indices := make([]int64, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, err := CreateOrder(context.Background(), pool, defaultParams(fmt.Sprintf("concurrent-%d", i), walletID))
			indices[i] = o.DerivationIndex
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[int64]bool, n)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateOrder() [%d] error = %v", i, err)
		}
		if indices[i] == 0 {
			t.Fatalf("CreateOrder() [%d] allocated index 0, must never happen", i)
		}
		if seen[indices[i]] {
			t.Fatalf("index %d allocated twice", indices[i])
		}
		seen[indices[i]] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct indices, want %d", len(seen), n)
	}
}

func TestToleranceBounds(t *testing.T) {
	tests := []struct {
		name      string
		target    int64
		percent   float64
		wantLower int64
		wantUpper int64
	}{
		{"1.5 percent", 100_000000, 1.5, 98_500000, 101_500000},
		{"zero tolerance", 100_000000, 0, 100_000000, 100_000000},
		{"tiny amount truncates down", 1, 50, 1, 1},                // bps=5000; tol=1*5000/10000=0（整數除法無條件捨去）
		{"fractional percent", 1_000000, 0.05, 999_500, 1_000_500}, // bps=round(5)=5; tol=1000000*5/10000=500
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lower, upper := toleranceBounds(tt.target, tt.percent)
			if lower != tt.wantLower || upper != tt.wantUpper {
				t.Errorf("toleranceBounds(%d, %v) = (%d, %d), want (%d, %d)", tt.target, tt.percent, lower, upper, tt.wantLower, tt.wantUpper)
			}
		})
	}
}
