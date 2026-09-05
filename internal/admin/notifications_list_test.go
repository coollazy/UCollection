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

func TestNotificationsListHandler_FiltersAndShowsOrderID(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	ord := newOrder(t, pool, walletID, "notif-1")

	var deliveryID int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO webhook_deliveries (event_id, order_id, event_type, payload, status)
		VALUES ('evt-1', $1, 'ORDER_COMPLETED', '{}', 'delivered')
		RETURNING id
	`, ord.ID).Scan(&deliveryID)
	if err != nil {
		t.Fatalf("insert webhook_deliveries: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/notifications", nil)
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

	if !strings.Contains(text, fmt.Sprintf("/admin/orders/%d", ord.ID)) {
		t.Errorf("body missing link to order %d, got: %s", ord.ID, text)
	}
	if !strings.Contains(text, "ORDER_COMPLETED") {
		t.Errorf("body missing event_type, got: %s", text)
	}

	// Filter by status that doesn't match — should show 0.
	req2, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/notifications?status=failed", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.AddCookie(cookie)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp2.Body.Close() })
	body2, _ := io.ReadAll(resp2.Body)
	if strings.Contains(string(body2), "ORDER_COMPLETED") {
		t.Errorf("filtered-out delivery should not appear, got: %s", body2)
	}
}
