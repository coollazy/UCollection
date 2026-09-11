package consolidation

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/coollazy/UCollection/internal/order"
)

// signCSPHeader is 驗證結論-08 already-validated 嚴格CSP value, applied
// verbatim (CLAUDE.md安全鐵律3：處理助記詞/私鑰輸入的頁面套用嚴格CSP，離線bundle，無
// inline script/CDN). Not derived from any request input, so it's a plain
// constant, not built per-request.
const signCSPHeader = "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// signItem is one order the operator selected — 技術架構設計第10節「對每個勾選
// 地址...」. Address's meaning depends on Type: in consolidation mode it's
// the source order's own address (the one whose balance gets drained, and
// what the post-signature self-check must match — CLAUDE.md安全鐵律4); in
// fee-topup mode it's the recipient of that item's TRX transfer (the
// signer/self-check address is FeeSourceAddress instead, shared by every
// item in the batch). In combined mode Address is again the source order's
// own address, and it doubles as both the TRX top-up recipient and the USDT
// drain source.
//
// The on-chain snapshot fields (USDTBalance/TRXBalance/AlreadyConsolidated)
// are populated ONLY for type="combined" (ADR-0017 決策6/7「最小持久化、靠鏈上
// 現況推導」可重入基礎): they give the Phase 4b frontend orchestrator each
// address's current standing so it can decide, on this or a re-entry visit,
// which item is the first-time (energy-expensive) consolidation, how much
// TRX each still needs, and which orders are already done. They stay zero
// for the legacy consolidation/fee-topup modes, which don't query the chain
// here. OnChainError flags an item whose on-chain lookup failed so the
// frontend can tell "genuinely zero / not yet consolidated" apart from
// "unknown, don't act on this yet" — a failed query never blocks the whole
// page from rendering.
type signItem struct {
	OrderID             int64  `json:"order_id"`
	Address             string `json:"address"`
	DerivationIndex     int64  `json:"derivation_index"`
	USDTBalance         int64  `json:"usdt_balance"`         // combined only, 最小單位 int64 (安全鐵律6)
	TRXBalance          int64  `json:"trx_balance"`          // combined only, sun int64
	AlreadyConsolidated bool   `json:"already_consolidated"` // combined only
	OnChainError        bool   `json:"onchain_error,omitempty"`
}

// signPageJSON is what gets embedded into consolidation_sign.html as
// {type="application/json"} text — never as template.JS/inline <script>
// (CLAUDE.md安全鐵律3). All fields here are non-secret (public addresses,
// xpub, IDs) — the browser still requires the operator's own mnemonic/
// private key input to do anything with them.
type signPageJSON struct {
	Type                  string     `json:"type"`                             // "consolidation" | "fee-topup" | "combined"
	BatchID               int64      `json:"batch_id,omitempty"`               // consolidation/fee-topup only
	ConsolidationBatchID  int64      `json:"consolidation_batch_id,omitempty"` // combined only
	FeeTopupBatchID       int64      `json:"fee_topup_batch_id,omitempty"`     // combined only
	MasterWalletID        int64      `json:"master_wallet_id"`
	Xpub                  string     `json:"xpub,omitempty"`                     // consolidation/combined
	DestinationAddress    string     `json:"destination_address,omitempty"`      // consolidation/combined
	FeeSource             string     `json:"fee_source,omitempty"`               // fee-topup/combined
	FeeSourceAddress      string     `json:"fee_source_address,omitempty"`       // fee-topup/combined
	DefaultAmountPerOrder int64      `json:"default_amount_per_order,omitempty"` // fee-topup legacy mode prefill only
	FirstAmountPerOrder   int64      `json:"first_amount_per_order,omitempty"`   // combined only：伺服器自動試算，非商戶輸入
	RepeatAmountPerOrder  int64      `json:"repeat_amount_per_order,omitempty"`  // combined only：伺服器自動試算，非商戶輸入
	Items                 []signItem `json:"items"`
}

type signPageData struct {
	Type         string
	PageDataJSON string // pre-marshaled JSON text, embedded via ordinary (non-JS-context) auto-escaping — see templates/consolidation_sign.html
}

var errInvalidSignType = errors.New("consolidation: type must be consolidation, fee-topup, or combined")

// signPageHandler implements GET /admin/consolidation/sign (技術架構設計第10節
// 「助記詞輸入與私鑰衍生」). Loads the batch the operator just created via
// POST .../batches or .../fee-topup-batches, resolves the order_ids carried
// in the query string into signItems, and renders the strict-CSP signing
// page — no private key material ever touches this handler or the pages it
// renders (CLAUDE.md安全鐵律1).
func signPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		kind := r.URL.Query().Get("type")
		if kind != "consolidation" && kind != "fee-topup" && kind != "combined" {
			http.Error(w, errInvalidSignType.Error(), http.StatusBadRequest)
			return
		}

		var masterWalletID int64
		page := signPageJSON{Type: kind}

		switch kind {
		case "consolidation":
			batchID, err := strconv.ParseInt(r.URL.Query().Get("batch_id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid batch_id", http.StatusBadRequest)
				return
			}
			page.BatchID = batchID

			batch, err := loadConsolidationBatch(ctx, deps.Pool, batchID)
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "batch not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			masterWalletID = batch.MasterWalletID
			page.DestinationAddress = batch.DestinationAddress

			wallet, err := getMasterWallet(ctx, deps.Pool, masterWalletID)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			page.Xpub = wallet.Xpub

		case "fee-topup":
			batchID, err := strconv.ParseInt(r.URL.Query().Get("batch_id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid batch_id", http.StatusBadRequest)
				return
			}
			page.BatchID = batchID

			batch, err := loadFeeTopupBatch(ctx, deps.Pool, batchID)
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "batch not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			masterWalletID = batch.MasterWalletID
			page.FeeSource = batch.FeeSource
			page.FeeSourceAddress = batch.FeeSourceAddress

			if amt, err := strconv.ParseInt(r.URL.Query().Get("amount_per_order"), 10, 64); err == nil && amt > 0 {
				page.DefaultAmountPerOrder = amt
			}

		case "combined":
			// The integrated flow (ADR-0017): one page carries BOTH the
			// consolidation batch (USDT drains, from the master wallet's HD
			// tree — needs Xpub + DestinationAddress) and the fee-topup batch
			// (TRX top-ups from the operator-specified source — needs
			// FeeSource/FeeSourceAddress). The consolidation batch's master
			// wallet is authoritative; the fee-topup batch is loaded only for
			// its source fields.
			consBatchID, err := strconv.ParseInt(r.URL.Query().Get("consolidation_batch_id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid consolidation_batch_id", http.StatusBadRequest)
				return
			}
			feeBatchID, err := strconv.ParseInt(r.URL.Query().Get("fee_topup_batch_id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid fee_topup_batch_id", http.StatusBadRequest)
				return
			}
			page.ConsolidationBatchID = consBatchID
			page.FeeTopupBatchID = feeBatchID

			consBatch, err := loadConsolidationBatch(ctx, deps.Pool, consBatchID)
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "consolidation batch not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			masterWalletID = consBatch.MasterWalletID
			page.DestinationAddress = consBatch.DestinationAddress

			feeBatch, err := loadFeeTopupBatch(ctx, deps.Pool, feeBatchID)
			if errors.Is(err, errRowNotFound) {
				http.Error(w, "fee-topup batch not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if feeBatch.MasterWalletID != masterWalletID {
				// Both batches are created together by createFlowHandler for the
				// same wallet, so this only happens if the two IDs were tampered
				// with in the URL to point at unrelated batches. Refuse rather
				// than render a page mixing two wallets' addresses.
				http.Error(w, "batches belong to different master wallets", http.StatusBadRequest)
				return
			}
			page.FeeSource = feeBatch.FeeSource
			page.FeeSourceAddress = feeBatch.FeeSourceAddress

			wallet, err := getMasterWallet(ctx, deps.Pool, masterWalletID)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			page.Xpub = wallet.Xpub

			// createFlowHandler已經自動試算好首筆/其餘筆金額並帶在query string裡
			// （2026-09-11使用者拍板：拿掉手動輸入欄位），這裡只是原樣讀出、不重算。
			// 缺任一值視為prepare-flow沒有正常走過（例如URL被手動竄改），讓兩者都
			// 是0——runCombinedFlow看到0會在補TRX步驟自然視為「不需要補」而非拋錯，
			// 不需要在這裡另外擋（該頁本來就要求逐筆核對衍生地址與已知地址相符）。
			if amt, err := strconv.ParseInt(r.URL.Query().Get("first_amount_per_order"), 10, 64); err == nil && amt > 0 {
				page.FirstAmountPerOrder = amt
			}
			if amt, err := strconv.ParseInt(r.URL.Query().Get("repeat_amount_per_order"), 10, 64); err == nil && amt > 0 {
				page.RepeatAmountPerOrder = amt
			}

			// 進頁先跑一次 ReconcileBroadcasting (ADR-0017 決策6「可重入」的基礎):
			// resolve any items still stuck in 'broadcasting' from a prior
			// interrupted run before we snapshot each address's on-chain
			// standing, so the snapshot the frontend orchestrates against
			// reflects settled state. A reconcile error must not blank the
			// whole page — log and carry on; the per-item snapshot below is
			// itself best-effort for the same reason.
			if err := ReconcileBroadcasting(ctx, deps, &masterWalletID); err != nil {
				log.Printf("consolidation: sign page (combined): reconcile broadcasting: %v", err)
			}
		}
		page.MasterWalletID = masterWalletID

		for _, idStr := range r.URL.Query()["order_id"] {
			orderID, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				continue
			}
			ord, err := order.GetByID(ctx, deps.Pool, orderID)
			if err != nil {
				log.Printf("consolidation: sign page: order %d not found, skipping: %v", orderID, err)
				continue
			}
			if ord.MasterWalletID != masterWalletID {
				log.Printf("consolidation: sign page: order %d belongs to a different master wallet, skipping", orderID)
				continue
			}
			item := signItem{
				OrderID:         ord.ID,
				Address:         ord.Address,
				DerivationIndex: ord.DerivationIndex,
			}
			if kind == "combined" {
				populateOnChainSnapshot(ctx, deps, ord, &item)
			}
			page.Items = append(page.Items, item)
		}

		pageJSON, err := json.Marshal(page)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Security-Policy", signCSPHeader)
		render(w, http.StatusOK, "consolidation_sign.html", signPageData{
			Type:         kind,
			PageDataJSON: string(pageJSON),
		})
	}
}

// populateOnChainSnapshot fills a combined-mode signItem's on-chain fields
// from live TronGrid reads plus the persisted consolidation_status. Every
// on-chain lookup is best-effort: a failure is logged, marks OnChainError,
// and leaves the balance at zero rather than failing the whole page —
// 技術架構設計第10節/ADR-0017 want a page that still renders (so the operator can
// see what did resolve) when one address's query hiccups. AlreadyConsolidated
// comes straight from the order row and never fails.
func populateOnChainSnapshot(ctx context.Context, deps Deps, ord order.Order, item *signItem) {
	item.AlreadyConsolidated = ord.ConsolidationStatus == order.ConsolidationConsolidated

	usdt, err := deps.TronClient.TRC20Balance(ctx, ord.Address, deps.USDTContractAddress)
	if err != nil {
		log.Printf("consolidation: sign page (combined): USDT balance for order %d (%s): %v", ord.ID, ord.Address, err)
		item.OnChainError = true
	} else {
		item.USDTBalance = usdt
	}

	trx, err := deps.TronClient.AccountTRXBalance(ctx, ord.Address)
	if err != nil {
		log.Printf("consolidation: sign page (combined): TRX balance for order %d (%s): %v", ord.ID, ord.Address, err)
		item.OnChainError = true
	} else {
		item.TRXBalance = trx
	}
}
