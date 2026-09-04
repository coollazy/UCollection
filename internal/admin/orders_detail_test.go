package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestOrderDetailHandler_AggregatesAcrossPackages(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "detail-1")

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, 'tx-detail', 0, 100000000, true, 1000)
	`, o.ID); err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}

	var batchID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO consolidation_batches (master_wallet_id, destination_address) VALUES ($1, 'TSeJkUh4Qv67VNFwY8LaAxERygNdy6NQZK') RETURNING id
	`, walletID).Scan(&batchID); err != nil {
		t.Fatalf("insert consolidation_batches: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO consolidation_items (batch_id, order_id, amount, tx_hash, broadcast_status)
		VALUES ($1, $2, 100000000, 'tx-consolidation-detail', 'success')
	`, batchID, o.ID); err != nil {
		t.Fatalf("insert consolidation_items: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+fmt.Sprintf("/admin/orders/%d?flash=order_created", o.ID), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	text := string(body)

	for _, want := range []string{"tx-detail", "tx-consolidation-detail", o.Address, "order_created"} {
		if !strings.Contains(text, want) {
			t.Errorf("body missing %q, got: %s", want, text)
		}
	}
}

func TestOrderDetailHandler_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/orders/99999", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
