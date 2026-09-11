package checkout

import "net/http"

// RegisterRoutes registers this module's two public (no-login) routes onto
// the shared top-level mux (技術架構設計第1節/第6節路由表), the same
// exact-pattern approach the other modules use:
//
//	GET /checkout/{token}         — full payment page
//	GET /checkout/{token}/status  — status fragment, polled every 5s by checkout.js
//
// Unlike admin/consolidation these carry no auth middleware — the
// unguessable public_token is the only access control (見第6節).
func RegisterRoutes(mux *http.ServeMux, deps Deps) {
	mux.Handle("GET /checkout/{token}", pageHandler(deps.Pool))
	mux.Handle("GET /checkout/{token}/status", statusHandler(deps.Pool))
}
