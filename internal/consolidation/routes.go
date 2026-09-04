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
	requireFreshTOTP := func(h http.HandlerFunc) http.Handler {
		return auth.RequireSession(authDeps)(auth.RequireFreshTOTP(authDeps)(h))
	}

	// /tron-proxy/... — 資金轉出端點，風險等級最高，全部要求TOTP新鮮度 (Part 1).
	mux.Handle("POST /tron-proxy/consolidation/prepare", requireFreshTOTP(prepareConsolidationHandler(deps)))
	mux.Handle("POST /tron-proxy/consolidation/broadcast", requireFreshTOTP(broadcastConsolidationHandler(deps)))
	mux.Handle("POST /tron-proxy/fee-topup/prepare", requireFreshTOTP(prepareFeeTopupHandler(deps)))
	mux.Handle("POST /tron-proxy/fee-topup/broadcast", requireFreshTOTP(broadcastFeeTopupHandler(deps)))

	// /admin/consolidation/... — 不碰金鑰的HTML頁面 (Part 2). 唯讀查詢維持
	// RequireSession；地址簿寫入（新增/改名/刪除）疊加RequireFreshTOTP，理由同
	// 技術架構設計第10節「地址簿的新增/改名/刪除則需要step-up」.
	mux.Handle("GET /admin/consolidation", requireSession(pendingListPageHandler(deps)))
	mux.Handle("GET /admin/consolidation/address-book", requireSession(addressBookPageHandler(deps)))
	mux.Handle("POST /admin/consolidation/address-book", requireFreshTOTP(addressBookSubmitHandler(deps)))
	mux.Handle("DELETE /admin/consolidation/address-book/{id}", requireFreshTOTP(addressBookDeleteHandler(deps)))
}
