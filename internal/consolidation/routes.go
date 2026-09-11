package consolidation

import (
	"net/http"
	"net/url"

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

	// toConsolidationPending：批次表單（開始歸集/補充手續費）送出失敗——例如
	// TOTP驗證碼錯誤——時，導回「該代收主錢包的待歸集頁」而非光禿的
	// /admin/consolidation。多錢包環境下後者會落到選錢包頁（consolidation_
	// wallets.html，不顯示任何提示），使用者只看到莫名跳回列表；待歸集頁本身
	// 已會顯示totp_error訊息（見totpReverifiedNotice）。master_wallet_id從送出
	// 的表單讀取——此時RequireTOTPCode尚未放行、handler未執行、body未被消耗，
	// PostFormValue已在讀totp_code時ParseForm，這裡讀的是快取後的表單值。
	toConsolidationPending := func(r *http.Request) string {
		if id := r.FormValue("master_wallet_id"); id != "" {
			return "/admin/consolidation?master_wallet_id=" + url.QueryEscape(id)
		}
		return "/admin/consolidation"
	}

	// /tron-proxy/... — 資金轉出端點，風險等級最高。page.js對一個批次的每個項目都
	// 各自呼叫prepare+broadcast（見sign頁流程），若比照其他路由要求每次都送驗證碼，
	// N筆批次就要輸入2N次，操作上不可行——改用RequireTOTPCodeOrRecentStepUp（見
	// ADR-0016「批次寬限」修訂）：批次內第一次呼叫仍要求新鮮驗證碼，之後
	// batchStepUpWindow內同一session的後續呼叫免驗證碼。
	mux.Handle("POST /tron-proxy/consolidation/prepare", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(prepareConsolidationHandler(deps))))
	mux.Handle("POST /tron-proxy/consolidation/broadcast", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(broadcastConsolidationHandler(deps))))
	mux.Handle("POST /tron-proxy/fee-topup/prepare", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(prepareFeeTopupHandler(deps))))
	mux.Handle("POST /tron-proxy/fee-topup/broadcast", auth.RequireSession(authDeps)(auth.RequireTOTPCodeOrRecentStepUp(authDeps, toConsolidationList)(broadcastFeeTopupHandler(deps))))

	// /tron-proxy/consolidation/transaction-info — despite the /tron-proxy/
	// prefix (elsewhere in this file reserved for fund-moving
	// prepare/broadcast calls), this one is a pure on-chain status *read*
	// (gettransactioninfobyid) with no broadcast side effect, so it stays
	// RequireSession-only, matching the other read-only endpoints below rather
	// than the TOTP/step-up group above.
	mux.Handle("POST /tron-proxy/consolidation/transaction-info", requireSession(transactionInfoHandler(deps)))

	// /admin/consolidation/... — 不碰金鑰的HTML頁面 (Part 2). 唯讀查詢維持
	// RequireSession；地址簿寫入（新增/改名/刪除）疊加RequireTOTPCode，理由同
	// 技術架構設計第10節「地址簿的新增/改名/刪除則需要step-up」.
	mux.Handle("GET /admin/consolidation", requireSession(pendingListPageHandler(deps)))
	mux.Handle("GET /admin/consolidation/address-book", requireSession(addressBookPageHandler(deps)))
	mux.Handle("POST /admin/consolidation/address-book", requireTOTPCode(addressBookSubmitHandler(deps), func(*http.Request) string { return "/admin/consolidation/address-book" }))
	mux.Handle("DELETE /admin/consolidation/address-book/{id}", requireTOTPCode(addressBookDeleteHandler(deps), func(*http.Request) string { return "/admin/consolidation/address-book" }))

	// 手續費來源地址簿（需求書5.9 v0.39）：與歸集地址簿為兩份獨立清單，權限分級
	// 完全比照——唯讀查詢 RequireSession，新增/改名/刪除疊加 RequireTOTPCode。
	mux.Handle("GET /admin/consolidation/fee-source-book", requireSession(feeSourceBookPageHandler(deps)))
	mux.Handle("POST /admin/consolidation/fee-source-book", requireTOTPCode(feeSourceBookSubmitHandler(deps), func(*http.Request) string { return "/admin/consolidation/fee-source-book" }))
	mux.Handle("DELETE /admin/consolidation/fee-source-book/{id}", requireTOTPCode(feeSourceBookDeleteHandler(deps), func(*http.Request) string { return "/admin/consolidation/fee-source-book" }))

	// /admin/consolidation/fee-estimate、/admin/consolidation/trx-balance —
	// 手動歸集流程重構Phase2（見docs/進度.md）新增的唯讀精算端點：對TronGrid做
	// triggerconstantcontract/getaccount模擬查詢，不建立任何consolidation_items/
	// fee_topup_items列、不簽名、不廣播，維持RequireSession，不疊加TOTP/step-up。
	mux.Handle("POST /admin/consolidation/fee-estimate", requireSession(feeEstimateHandler(deps)))
	mux.Handle("POST /admin/consolidation/trx-balance", requireSession(trxBalanceHandler(deps)))

	// /admin/consolidation/batches、/admin/consolidation/fee-topup-batches、
	// /admin/consolidation/sign — 助記詞簽名流程 (Part 3)。批次建立本身是資金操作的
	// 起手式、簽名頁會顯示xpub並接手瀏覽器端衍生/簽名，皆比照CLAUDE.md安全鐵律9套用
	// RequireTOTPCode，不能只掛一般登入session。
	mux.Handle("POST /admin/consolidation/batches", requireTOTPCode(createConsolidationBatchHandler(deps), toConsolidationPending))
	mux.Handle("POST /admin/consolidation/fee-topup-batches", requireTOTPCode(createFeeTopupBatchHandler(deps), toConsolidationPending))
	// /admin/consolidation/prepare-flow、GET .../sign — 手動歸集流程重構Phase4
	// （ADR-0017）：填寫頁合併後的單一「準備歸集」submit 送到prepare-flow，一次建立
	// consolidation batch + fee-topup batch 並導向整合簽名頁（type=combined）。舊的
	// batches/fee-topup-batches 路由保留不刪（既有測試與相容性），只是填寫頁不再送到
	// 它們。**這兩條刻意只掛 RequireSession，不疊加 RequireTOTPCode**
	// （2026-09-11使用者拍板，見createFlowHandler doc comment的完整理由）：TOTP
	// 驗證整個collapse到簽名頁自己的totp-code-input欄位（由第一次
	// /tron-proxy/.../prepare呼叫消耗），避免這裡跟簽名頁各自要求一次、外加GET
	// 簽名頁原本掛的reverify-totp又插一次，變成三次。
	mux.Handle("POST /admin/consolidation/prepare-flow", requireSession(createFlowHandler(deps)))
	mux.Handle("GET /admin/consolidation/sign", requireSession(signPageHandler(deps)))
}
