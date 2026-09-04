package admin

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/base58"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func toHexAddress(t *testing.T, base58Address string) string {
	t.Helper()
	raw, _, err := base58.CheckDecode(base58Address)
	if err != nil {
		t.Fatalf("base58.CheckDecode(%q): %v", base58Address, err)
	}
	return hex.EncodeToString(raw)
}

func eventsResponseJSON(toHex string) string {
	return fmt.Sprintf(`{"data":[{"transaction_id":"tx-reverify","event_index":0,"block_number":1000,"block_timestamp":%d,"result":{"from":"1111111111111111111111111111111111111111","to":%q,"value":"100000000"}}],"success":true,"meta":{}}`,
		1000, toHex)
}

// TestReverifyHandler_MergesButDoesNotEvaluate is an integration-level
// check (through the real HTTP mux, real DB, mocked TronGrid) that
// scanner.ReverifyOrder is actually wired up correctly and that the audit
// trail lands regardless of outcome (技術架構設計第11節「手動重新檢查此地址」).
// The no-auto-evaluation behavior itself is unit-tested exhaustively in
// internal/scanner/reverify_test.go — this only needs to prove the wiring.
func TestReverifyHandler_MergesButDoesNotEvaluate(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "reverify-handler-1")

	toHex := toHexAddress(t, o.Address)
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(eventsResponseJSON(toHex)))
	}))
	t.Cleanup(mockSrv.Close)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient(mockSrv.URL, ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+fmt.Sprintf("/admin/orders/%d/reverify", o.ID), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash=reverify_ok") {
		t.Errorf("Location = %q, want flash=reverify_ok", got)
	}

	var txCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM incoming_transactions WHERE order_id = $1`, o.ID).Scan(&txCount); err != nil {
		t.Fatalf("count incoming_transactions: %v", err)
	}
	if txCount != 1 {
		t.Errorf("incoming_transactions count = %d, want 1", txCount)
	}

	status, err := order.GetByID(context.Background(), pool, o.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if status.Status != order.StatusPending {
		t.Errorf("status = %s, want PENDING (reverify must not auto-evaluate)", status.Status)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'ORDER_MANUAL_REVERIFY'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("ORDER_MANUAL_REVERIFY audit_logs count = %d, want 1", auditCount)
	}
}
