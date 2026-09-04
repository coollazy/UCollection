package consolidation

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/auth"
)

// RegisterRoutes registers this module's own /tron-proxy/... routes
// (技術架構設計第10節路由表) directly onto the shared top-level mux — the same
// exact-pattern-registration approach internal/auth uses, so admin/other
// modules can register their own routes on the same mux without prefix
// conflicts. All four routes require RequireSession+RequireFreshTOTP(15分
// 鐘)（技術架構設計第10節：這些端點涉及實際資金轉出，風險等級最高）.
func RegisterRoutes(mux *http.ServeMux, deps Deps, authDeps auth.Deps) {
	protect := func(h http.HandlerFunc) http.Handler {
		return auth.RequireSession(authDeps)(auth.RequireFreshTOTP(authDeps)(h))
	}

	mux.Handle("POST /tron-proxy/consolidation/prepare", protect(prepareConsolidationHandler(deps)))
	mux.Handle("POST /tron-proxy/consolidation/broadcast", protect(broadcastConsolidationHandler(deps)))
	mux.Handle("POST /tron-proxy/fee-topup/prepare", protect(prepareFeeTopupHandler(deps)))
	mux.Handle("POST /tron-proxy/fee-topup/broadcast", protect(broadcastFeeTopupHandler(deps)))
}
