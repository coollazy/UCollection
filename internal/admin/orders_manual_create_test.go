package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestParseUSDTAmount(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"100", 100_000000, false},
		{"100.50", 100_500000, false},
		{"0.000001", 1, false},
		{"1.2345678", 0, true}, // 7 decimals, rejected
		{"-5", 0, true},        // negative
		{"", 0, true},          // empty
		{"0", 0, true},         // must be > 0
		{"abc", 0, true},       // not a number
	}
	for _, c := range cases {
		got, err := parseUSDTAmount(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseUSDTAmount(%q) = %d, nil, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseUSDTAmount(%q) error = %v, want nil", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseUSDTAmount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestManualCreateSubmitHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	newMasterWallet(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"merchant_order_no": {"manual-1"}, "target_amount": {"100.50"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/orders", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}

	got, err := order.GetByMerchantOrderNo(context.Background(), pool, "manual-1")
	if err != nil {
		t.Fatalf("GetByMerchantOrderNo() error = %v", err)
	}
	if got.TargetAmount != 100_500000 {
		t.Errorf("TargetAmount = %d, want 100500000", got.TargetAmount)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'ORDER_MANUAL_CREATED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("ORDER_MANUAL_CREATED audit_logs count = %d, want 1", auditCount)
	}
}

func TestManualCreateSubmitHandler_DuplicateSameAmountIsIdempotent(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	newMasterWallet(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	submit := func(amount string) *http.Response {
		form := url.Values{"merchant_order_no": {"manual-dup"}, "target_amount": {amount}}
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/orders", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		resp, err := noRedirectClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		return resp
	}

	resp1 := submit("50")
	_ = resp1.Body.Close()
	loc1 := strings.SplitN(resp1.Header.Get("Location"), "?", 2)[0]

	resp2 := submit("50")
	_ = resp2.Body.Close()
	loc2 := strings.SplitN(resp2.Header.Get("Location"), "?", 2)[0]
	// Same order path (the flash query differs deliberately: order_created
	// vs order_exists), and only one order must actually have been created.
	if loc1 != loc2 {
		t.Errorf("idempotent resubmit redirected to a different order: %q vs %q", loc1, loc2)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM orders WHERE merchant_order_no = 'manual-dup'`).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 1 {
		t.Errorf("orders with merchant_order_no=manual-dup = %d, want 1 (idempotent, not double-created)", count)
	}

	resp3 := submit("99")
	t.Cleanup(func() { _ = resp3.Body.Close() })
	if resp3.StatusCode != http.StatusSeeOther {
		t.Fatalf("conflicting amount resubmit status = %d, want 303", resp3.StatusCode)
	}
	if got := resp3.Header.Get("Location"); !strings.Contains(got, "/admin/orders?flash_error=") {
		t.Errorf("conflicting amount resubmit Location = %q, want redirect back to list with flash_error", got)
	}
}

func TestManualCreateSubmitHandler_NoActiveMasterWallet(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	// deliberately no newMasterWallet(t, pool) call
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	form := url.Values{"target_amount": {"100"}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/orders", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); !strings.Contains(got, "flash_error") {
		t.Errorf("Location = %q, want flash_error redirect (RequireMasterWallet Gate)", got)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM orders`).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 0 {
		t.Errorf("orders count = %d, want 0 (no order should have been created)", count)
	}
}
