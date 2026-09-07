package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/consolidation"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/webhook"
)

type orderDetailPageData struct {
	Order                order.Order
	Transitions          []order.Transition
	IncomingTransactions []order.IncomingTransaction
	WebhookDeliveries    []webhook.DeliveryWithAttempts
	ConsolidationItems   []consolidation.ConsolidationItem
	FeeTopupItems        []consolidation.FeeTopupItem
	ManualWhitelist      []order.Status
	Flash                string
	FlashError           string
	TOTPReverifiedNotice string
}

// orderDetailHandler implements GET /admin/orders/{id} (技術架構設計第11節「訂單
// 列表與明細」明細頁段落). Reads ?flash=/?flash_error= for the banner shown
// after reverify/override-status POST back here (沿用整站無JS/plain-form-
// redirect風格，見internal/consolidation既有頁面).
func orderDetailHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid order id", http.StatusBadRequest)
			return
		}

		ord, err := order.GetByID(ctx, deps.Pool, id)
		if errors.Is(err, order.ErrOrderNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		transitions, err := order.Transitions(ctx, deps.Pool, id)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		incoming, err := order.ListIncomingTransactions(ctx, deps.Pool, id)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		deliveries, err := webhook.ListForOrder(ctx, deps.Pool, id)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		items, feeTopups, err := consolidation.ItemsForOrder(ctx, deps.Pool, id)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "order_detail.html", orderDetailPageData{
			Order:                ord,
			Transitions:          transitions,
			IncomingTransactions: incoming,
			WebhookDeliveries:    deliveries,
			ConsolidationItems:   items,
			FeeTopupItems:        feeTopups,
			ManualWhitelist:      manualTargetsFor(ord.Status),
			Flash:                r.URL.Query().Get("flash"),
			FlashError:           r.URL.Query().Get("flash_error"),
			TOTPReverifiedNotice: totpReverifiedNotice(r),
		})
	}
}

// manualTargetsFor mirrors order.manualWhitelist (unexported) just enough
// to drive the override-status form's <select> options — kept as a small
// local literal rather than exporting order's internal whitelist map,
// since order.ManualTransition is still the single source of truth that
// actually enforces it server-side (this is UI convenience only).
func manualTargetsFor(from order.Status) []order.Status {
	switch from {
	case order.StatusConfirmationStalled:
		return []order.Status{order.StatusCompleted, order.StatusOverpaid, order.StatusExpired}
	case order.StatusExpired:
		return []order.Status{order.StatusCompleted, order.StatusOverpaid}
	default:
		return nil
	}
}
