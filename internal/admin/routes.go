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

	// 代收主錢包設定 (Part 2)：切換使用中代收主錢包＝劫持新收款流向，新增/新增頁/
	// 恢復使用中三個寫入操作皆疊加RequireFreshTOTP（技術架構設計第11節「權限分級理由」
	// 判準(1)/(2)）。GET .../new本身就是助記詞輸入頁面，比照第10節sign頁先例同等級保護。
	mux.Handle("GET /admin/master-wallets", requireSession(masterWalletsListHandler(deps)))
	mux.Handle("GET /admin/master-wallets/new", requireFreshTOTP(masterWalletNewPageHandler(deps)))
	mux.Handle("POST /admin/master-wallets", requireFreshTOTP(createMasterWalletHandler(deps)))
	mux.Handle("POST /admin/master-wallets/{id}/reactivate", requireFreshTOTP(reactivateMasterWalletHandler(deps)))

	// API Key 管理 (Part 2)：重新產生會讓新key/secret冒用商戶身分呼叫API，風險同代收
	// 主錢包切換，疊加RequireFreshTOTP。
	mux.Handle("GET /admin/api-keys", requireSession(apiKeysListHandler(deps)))
	mux.Handle("POST /admin/api-keys/regenerate", requireFreshTOTP(regenerateAPIKeyHandler(deps)))

	// Webhook 端點設定 (Part 2)：URL被竄改重導向、secret外洩可偽造終態通知，風險同代收
	// 主錢包切換，疊加RequireFreshTOTP；test僅同步送一次、不落地、不改變任何持久狀態，
	// RequireSession即可。
	mux.Handle("GET /admin/webhook-config", requireSession(webhookConfigPageHandler(deps)))
	mux.Handle("POST /admin/webhook-config", requireFreshTOTP(updateWebhookURLHandler(deps)))
	mux.Handle("POST /admin/webhook-config/secret", requireFreshTOTP(updateWebhookSecretHandler(deps)))
	mux.Handle("POST /admin/webhook-config/test", requireSession(testWebhookHandler(deps)))

	// 參數設定 (Part 2)：僅影響之後新建立的訂單，不涉及資金/身分驗簽材料外洩風險，
	// RequireSession即可。
	mux.Handle("GET /admin/params", requireSession(paramsPageHandler(deps)))
	mux.Handle("POST /admin/params", requireSession(updateParamsHandler(deps)))
}
