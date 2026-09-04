package admin

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestOrdersListHandler_StatusFilter(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	pending := newOrder(t, pool, walletID, "list-pending")
	completed := newOrder(t, pool, walletID, "list-completed")
	if _, err := pool.Exec(t.Context(), `UPDATE orders SET status = 'COMPLETED' WHERE id = $1`, completed.ID); err != nil {
		t.Fatalf("force completed: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/orders?status=COMPLETED", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, completed.MerchantOrderNo) {
		t.Errorf("body missing completed order, got: %s", text)
	}
	if strings.Contains(text, pending.MerchantOrderNo) {
		t.Errorf("body should not contain filtered-out pending order, got: %s", text)
	}
}
