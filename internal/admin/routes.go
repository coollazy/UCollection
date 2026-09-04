package admin

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/auth"
)

// RegisterRoutes registers this Part's routes directly onto the shared
// top-level mux (同 internal/consolidation/routes.go 的精確路徑註冊模式，見
// 技術架構設計第11節「路由總覽」).
func RegisterRoutes(mux *http.ServeMux, deps Deps, authDeps auth.Deps) {
	requireSession := func(h http.HandlerFunc) http.Handler {
		return auth.RequireSession(authDeps)(h)
	}
	requireFreshTOTP := func(h http.HandlerFunc) http.Handler {
		return auth.RequireSession(authDeps)(auth.RequireFreshTOTP(authDeps)(h))
	}

	mux.Handle("GET /admin/dashboard", requireSession(dashboardHandler(deps)))

	mux.Handle("GET /admin/orders", requireSession(ordersListHandler(deps)))
	mux.Handle("GET /admin/orders/{id}", requireSession(orderDetailHandler(deps)))
	// 技術架構設計第11節路由表只列了POST /admin/orders，沒有獨立的GET .../new——
	// 比照既有 internal/consolidation 慣例（例：address-book 頁面本身就內嵌新增表單），
	// 手動建單表單直接嵌在 GET /admin/orders（orders_list.html）裡，不另開頁面。
	// RequireMasterWallet Gate 內嵌在 manualCreateSubmitHandler 開頭，不是獨立middleware
	// ——見該handler的doc comment。
	mux.Handle("POST /admin/orders", requireSession(manualCreateSubmitHandler(deps)))
	mux.Handle("POST /admin/orders/{id}/reverify", requireSession(reverifyHandler(deps)))

	// override-status無伺服器端金額防線、成功即觸發真實終態Webhook，故疊加
	// RequireFreshTOTP（技術架構設計第11節「權限分級理由」判準(3)）。
	mux.Handle("POST /admin/orders/{id}/override-status", requireFreshTOTP(overrideStatusHandler(deps)))
}
