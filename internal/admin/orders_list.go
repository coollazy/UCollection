package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/coollazy/UCollection/internal/order"
)

const ordersPageSize = 50

type ordersListPageData struct {
	Orders                  []order.Order
	Filter                  order.ListFilter
	Page                    int
	TotalPages              int
	Total                   int
	PrevPage                int
	NextPage                int
	HasPrev                 bool
	HasNext                 bool
	AllStatuses             []order.Status
	SelectedStatus          map[order.Status]bool
	NewOrderMerchantOrderNo string
	FlashError              string
}

// ordersListHandler implements GET /admin/orders (技術架構設計第11節「訂單列表與
// 明細」：狀態複選、merchant_order_no模糊查詢、address精確比對、代收主錢包、建立時間
// 範圍；預設依created_at降冪排序，offset+limit分頁).
func ordersListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		page := 1
		if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
			page = p
		}

		f := order.ListFilter{
			MerchantOrderNo: q.Get("merchant_order_no"),
			Address:         q.Get("address"),
			Offset:          (page - 1) * ordersPageSize,
			Limit:           ordersPageSize,
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

		orders, total, err := order.ListOrders(r.Context(), deps.Pool, f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + ordersPageSize - 1) / ordersPageSize
		if totalPages == 0 {
			totalPages = 1
		}

		selected := make(map[order.Status]bool, len(f.Statuses))
		for _, s := range f.Statuses {
			selected[s] = true
		}

		render(w, http.StatusOK, "orders_list.html", ordersListPageData{
			Orders:                  orders,
			Filter:                  f,
			Page:                    page,
			TotalPages:              totalPages,
			Total:                   total,
			PrevPage:                page - 1,
			NextPage:                page + 1,
			HasPrev:                 page > 1,
			HasNext:                 page < totalPages,
			AllStatuses:             allStatuses,
			SelectedStatus:          selected,
			NewOrderMerchantOrderNo: generateManualOrderNo(),
			FlashError:              q.Get("flash_error"),
		})
	}
}

var allStatuses = []order.Status{
	order.StatusPending,
	order.StatusConfirming,
	order.StatusCompleted,
	order.StatusOverpaid,
	order.StatusConfirmationStalled,
	order.StatusExpired,
}
