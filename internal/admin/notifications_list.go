package admin

import (
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/webhook"
)

const notificationsPageSize = 50

var allDeliveryStatuses = []string{"awaiting_config", "pending", "sending", "delivered", "failed"}

type notificationsListPageData struct {
	Deliveries  []webhook.DeliveryWithAttempts
	Filter      webhook.ListFilter
	Page        int
	TotalPages  int
	Total       int
	PrevPage    int
	NextPage    int
	HasPrev     bool
	HasNext     bool
	AllStatuses []string
	Flash       string
	FlashError  string
}

// notificationsListHandler implements GET /admin/notifications (技術架構設計
// 第11節「通知歷史查詢與手動重發」：依訂單、event_type、status篩選). Pagination
// mirrors orders_list.go's ordersListHandler exactly (same field names/
// calculation, just a different page-size constant and underlying query).
func notificationsListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		page := 1
		if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
			page = p
		}

		f := webhook.ListFilter{
			EventType: q.Get("event_type"),
			Status:    q.Get("status"),
			Offset:    (page - 1) * notificationsPageSize,
			Limit:     notificationsPageSize,
		}
		if v := q.Get("order_id"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				f.OrderID = &id
			}
		}

		deliveries, total, err := webhook.ListAll(r.Context(), deps.Pool, f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + notificationsPageSize - 1) / notificationsPageSize
		if totalPages == 0 {
			totalPages = 1
		}

		render(w, http.StatusOK, "notifications.html", notificationsListPageData{
			Deliveries:  deliveries,
			Filter:      f,
			Page:        page,
			TotalPages:  totalPages,
			Total:       total,
			PrevPage:    page - 1,
			NextPage:    page + 1,
			HasPrev:     page > 1,
			HasNext:     page < totalPages,
			AllStatuses: allDeliveryStatuses,
			Flash:       q.Get("flash"),
			FlashError:  q.Get("flash_error"),
		})
	}
}
