package consolidation

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/auth"
)

// RegisterRoutes registers this module's own /tron-proxy/... and
// /admin/consolidation/... routes (技術架構設計第10節路由表) directly onto the
// shared top-level mux — the same exact-pattern-registration approach
// internal/auth uses, so admin/other modules can register their own routes
// on the same mux without prefix conflicts.
func RegisterRoutes(mux *http.ServeMux, deps Deps, authDeps auth.Deps) {
	requireSession := func(h http.HandlerFunc) http.Handler {
		return auth.RequireSession(authDeps)(h)
	}
	// requireTOTPCode's returnTo must be a GET-navigable page, never the
	// protected POST/DELETE route itself — see auth.RequireTOTPCode's doc
	// comment (found 2026-09-07: a 405 on a step-up-gated POST after a stale
	// session, see docs/進度.md). For the /tron-proxy/... endpoints (called
	// via fetch() from page.js, not a native form) there is no form to
	// resubmit anyway; returnTo just needs to be a safe, valid landing page
	// rather than a route that itself 404/405s if ever hit directly.
	requireTOTPCode := func(h http.HandlerFunc, returnTo func(*http.Request) string) http.Handler {
		return auth.RequireSession(authDeps)(auth.RequireTOTPCode(authDeps, returnTo)(h))
	}
	toConsolidationList := func(*http.Request) string { return "/admin/consolidation" }

	// /tron-proxy/... — 資金轉出端點，風險等級最高。page.js對一個批次的每個項目都
	// 各自呼叫prepare+broadcast（見sign頁流程），若比照其他路由要求每次都送驗證碼，
	// N筆批次就要輸入2N次，操作上不可行——改用RequireTOTPCodeOrRecentStepUp（見
	// ADR-0016「批次寬限」修訂）：批次內第一次呼叫仍要求新鮮驗證碼，之後
	// batchStepUpWindow內同一session的後續呼叫免驗證碼。
	mux.Handle("POST /tron-proxy/consolidation/prepare", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(prepareConsolidationHandler(deps))))
	mux.Handle("POST /tron-proxy/consolidation/broadcast", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(broadcastConsolidationHandler(deps))))
	mux.Handle("POST /tron-proxy/fee-topup/prepare", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(prepareFeeTopupHandler(deps))))
	mux.Handle("POST /tron-proxy/fee-topup/broadcast", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(broadcastFeeTopupHandler(deps))))

	// /admin/consolidation/... — 不碰金鑰的HTML頁面 (Part 2). 唯讀查詢維持
	// RequireSession；地址簿寫入（新增/改名/刪除）疊加RequireTOTPCode，理由同
	// 技術架構設計第10節「地址簿的新增/改名/刪除則需要step-up」.
	mux.Handle("GET /admin/consolidation", requireSession(pendingListPageHandler(deps)))
	mux.Handle("GET /admin/consolidation/address-book", requireSession(addressBookPageHandler(deps)))
	mux.Handle("POST /admin/consolidation/address-book", requireTOTPCode(addressBookSubmitHandler(deps), func(*http.Request) string { return "/admin/consolidation/address-book" }))
	mux.Handle("DELETE /admin/consolidation/address-book/{id}", requireTOTPCode(addressBookDeleteHandler(deps), func(*http.Request) string { return "/admin/consolidation/address-book" }))

	// /admin/consolidation/batches、/admin/consolidation/fee-topup-batches、
	// /admin/consolidation/sign — 助記詞簽名流程 (Part 3)。批次建立本身是資金操作的
	// 起手式、簽名頁會顯示xpub並接手瀏覽器端衍生/簽名，皆比照CLAUDE.md安全鐵律9套用
	// RequireTOTPCode，不能只掛一般登入session。
	mux.Handle("POST /admin/consolidation/batches", requireTOTPCode(createConsolidationBatchHandler(deps), toConsolidationList))
	mux.Handle("POST /admin/consolidation/fee-topup-batches", requireTOTPCode(createFeeTopupBatchHandler(deps), toConsolidationList))
	mux.Handle("GET /admin/consolidation/sign", requireTOTPCode(signPageHandler(deps), auth.SelfPath))
}
