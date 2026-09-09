package admin

import (
	"fmt"
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

// TestOrdersListHandler_PaginationAndExportCarryAllFilters 覆蓋第12項：翻頁表單與
// 匯出表單過去只帶 merchant_order_no/address/status 三個 hidden 欄位，master_wallet_id
// 與建立時間區間會在翻頁/匯出時被靜默丟掉。這裡驗證三個進階篩選條件都被帶出。
func TestOrdersListHandler_PaginationAndExportCarryAllFilters(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "list-carryfilter")

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	path := fmt.Sprintf("/admin/orders?master_wallet_id=%d&created_from=2020-01-01&created_to=2999-12-31", walletID)
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
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

	if !strings.Contains(text, o.MerchantOrderNo) {
		t.Fatalf("order should be within filter range, got: %s", text)
	}

	// 每個進階篩選條件都應出現在翻頁表單與匯出表單的 hidden 欄位裡（頂部篩選表單
	// 也會回填同值，因此至少出現兩次）。
	wantHiddens := []string{
		fmt.Sprintf(`name="master_wallet_id" value="%d"`, walletID),
		`name="created_from" value="2020-01-01"`,
		`name="created_to" value="2999-12-31"`,
	}
	for _, h := range wantHiddens {
		if n := strings.Count(text, h); n < 2 {
			t.Errorf("filter %q must be carried by both pagination and export forms, appeared %d times", h, n)
		}
	}
}
