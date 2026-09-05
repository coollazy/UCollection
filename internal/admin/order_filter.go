package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/coollazy/UCollection/internal/order"
)

// parseOrderListFilter extracts the WHERE-relevant fields of order.ListFilter
// from r's query string (status/merchant_order_no/address/master_wallet_id/
// created_from/created_to) — shared by both GET /admin/orders and GET
// /admin/export/orders so the export "套用與/admin/orders相同的篩選條件"
// (技術架構設計第11節) is one function, not two copies of the same parsing
// logic. Offset/Limit are NOT set here — callers have different pagination
// needs (a page size vs a streaming batch size) and set those themselves.
func parseOrderListFilter(r *http.Request) order.ListFilter {
	q := r.URL.Query()

	f := order.ListFilter{
		MerchantOrderNo: q.Get("merchant_order_no"),
		Address:         q.Get("address"),
	}
	for _, s := range q["status"] {
		f.Statuses = append(f.Statuses, order.Status(s))
	}
	if v := q.Get("master_wallet_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.MasterWalletID = &id
		}
	}
	if v := q.Get("created_from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.CreatedFrom = &t
		}
	}
	if v := q.Get("created_to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			t = t.Add(24 * time.Hour)
			f.CreatedTo = &t
		}
	}
	return f
}
