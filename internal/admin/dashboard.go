package admin

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/scanner"
	"github.com/coollazy/UCollection/internal/webhook"
)

type dashboardPageData struct {
	Stats                 order.DashboardStats
	Sync                  scanner.SyncStatus
	StuckCount            int
	WebhookFailing        int
	WebhookAwaitingConfig int
}

// dashboardHandler implements GET /admin/dashboard (技術架構設計第11節「儀表板」).
func dashboardHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		stats, err := order.GetDashboardStats(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		sync, err := scanner.GetSyncStatus(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		stuck, err := scanner.StuckCount(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		failing, awaitingConfig, err := webhook.PendingSummary(ctx, deps.Pool)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "dashboard.html", dashboardPageData{
			Stats:                 stats,
			Sync:                  sync,
			StuckCount:            stuck,
			WebhookFailing:        failing,
			WebhookAwaitingConfig: awaitingConfig,
		})
	}
}
