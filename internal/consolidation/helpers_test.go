package consolidation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

const testXpub = "xpub6D1AabNHCupeiLM65ZR9UStMhJ1vCpyV4XbZdyhMZBiJXALQtmn9p42VTQckoHVn8WNqS7dqnJokZHAHcHGoaQgmv8D45oNUKx6DZMNZBCd"

// Real Tron addresses reused from 驗證結論-06/08 (already cross-validated
// elsewhere in this codebase), used here purely as syntactically valid
// Base58Check addresses — no funds are ever touched in these tests.
const (
	testSourceAddress      = "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH"
	testDestinationAddress = "TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK"
	testUSDTContract       = "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs"
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
	_, err := pool.Exec(context.Background(), `
		TRUNCATE consolidation_address_book, consolidation_items, consolidation_batches,
			fee_topup_items, fee_topup_batches, master_wallets, orders,
			order_state_transitions, incoming_transactions, webhook_deliveries, audit_logs
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

func newMasterWallet(t *testing.T, pool *store.Pool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO master_wallets (xpub) VALUES ($1) RETURNING id`, testXpub).Scan(&id)
	if err != nil {
		t.Fatalf("insert master_wallets: %v", err)
	}
	return id
}

func newOrder(t *testing.T, pool *store.Pool, walletID int64, merchantOrderNo string) order.Order {
	t.Helper()
	o, err := order.CreateOrder(context.Background(), pool, order.CreateParams{
		MerchantOrderNo:                 merchantOrderNo,
		MasterWalletID:                  walletID,
		TargetAmount:                    100_000000,
		ValiditySeconds:                 900,
		AmountTolerancePercent:          1.5,
		ConfirmationStallTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("order.CreateOrder() error = %v", err)
	}
	return o
}

// mockResponse is one canned HTTP response.
type mockResponse struct {
	status int
	body   string
}

// mockTronGrid is a minimal fake TronGrid: each path has its own FIFO
// queue of canned responses, consumed one per matching request. Tests
// enqueue exactly the responses they expect for the sequence of calls the
// handler under test will make (e.g. balance query, then
// triggersmartcontract, then broadcasttransaction).
type mockTronGrid struct {
	mu        sync.Mutex
	responses map[string][]mockResponse
}

func newMockTronGrid() *mockTronGrid {
	return &mockTronGrid{responses: make(map[string][]mockResponse)}
}

func (m *mockTronGrid) enqueue(pathPrefix string, status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[pathPrefix] = append(m.responses[pathPrefix], mockResponse{status: status, body: body})
}

func (m *mockTronGrid) start(t *testing.T) *tronclient.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		var matchedKey string
		for key := range m.responses {
			if len(r.URL.Path) >= len(key) && r.URL.Path[:len(key)] == key {
				matchedKey = key
				break
			}
		}
		queue := m.responses[matchedKey]
		if len(queue) == 0 {
			t.Errorf("mockTronGrid: unexpected request %s %s (no queued response)", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := queue[0]
		m.responses[matchedKey] = queue[1:]
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	return tronclient.NewClient(srv.URL, "")
}
