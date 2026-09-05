package admin

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
	"github.com/xuri/excelize/v2"
)

func TestExportOrdersHandler_CSV(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o1 := newOrder(t, pool, walletID, "export-1")
	o2 := newOrder(t, pool, walletID, "export-2")
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status = 'CONFIRMATION_STALLED' WHERE id = $1`, o2.ID); err != nil {
		t.Fatalf("force STALLED: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO incoming_transactions (order_id, tx_hash, log_index, amount, confirmed, block_number)
		VALUES ($1, 'tx-export-1', 0, 40_000000, true, 1)
	`, o1.ID); err != nil {
		t.Fatalf("insert incoming_transactions: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/export/orders?format=csv", nil)
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
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	const utf8BOM = "\uFEFF"
	body := readAllString(t, resp)
	body = strings.TrimPrefix(body, utf8BOM)
	reader := csv.NewReader(strings.NewReader(body))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 { // header + 2 orders
		t.Fatalf("got %d records, want 3 (header+2)", len(records))
	}
	if records[0][0] != "訂單ID" {
		t.Errorf("header[0] = %q, want 訂單ID", records[0][0])
	}

	byOrderID := map[string][]string{}
	for _, rec := range records[1:] {
		byOrderID[rec[0]] = rec
	}
	rec1, ok := byOrderID[strconv.FormatInt(o1.ID, 10)]
	if !ok {
		t.Fatalf("order 1 row missing, got: %v", records)
	}
	if rec1[5] != "40000000" { // 已確認累計金額 column
		t.Errorf("order1 confirmed amount = %q, want 40000000", rec1[5])
	}
	if rec1[8] != "" { // 完成時間 column, non-terminal must be blank
		t.Errorf("order1 completed time = %q, want blank (non-terminal PENDING)", rec1[8])
	}

	rec2, ok := byOrderID[strconv.FormatInt(o2.ID, 10)]
	if !ok {
		t.Fatalf("order 2 row missing, got: %v", records)
	}
	if rec2[6] != "CONFIRMATION_STALLED" {
		t.Errorf("order2 status = %q, want CONFIRMATION_STALLED", rec2[6])
	}
}

func TestExportOrdersHandler_XLSX(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	newOrder(t, pool, walletID, "export-xlsx-1")

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/export/orders?format=xlsx", nil)
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
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Errorf("Content-Type = %q, want xlsx mime type", ct)
	}

	xf, err := excelize.OpenReader(resp.Body)
	if err != nil {
		t.Fatalf("excelize.OpenReader() error = %v", err)
	}
	defer func() { _ = xf.Close() }()

	rows, err := xf.GetRows("Sheet1")
	if err != nil {
		t.Fatalf("GetRows() error = %v", err)
	}
	if len(rows) != 2 { // header + 1 order
		t.Fatalf("got %d rows, want 2 (header+1)", len(rows))
	}
	if rows[0][0] != "訂單ID" {
		t.Errorf("header[0] = %q, want 訂單ID", rows[0][0])
	}
}

func TestExportOrdersHandler_InvalidFormat(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/export/orders?format=pdf", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestExceedsExportCap(t *testing.T) {
	if exceedsExportCap(exportMaxRows) {
		t.Errorf("exceedsExportCap(%d) = true, want false (at the cap, not over it)", exportMaxRows)
	}
	if !exceedsExportCap(exportMaxRows + 1) {
		t.Errorf("exceedsExportCap(%d) = false, want true (one over the cap)", exportMaxRows+1)
	}
}

func readAllString(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
