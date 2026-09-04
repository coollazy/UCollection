package consolidation

import (
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
// item in the batch).
type signItem struct {
	OrderID         int64  `json:"order_id"`
	Address         string `json:"address"`
	DerivationIndex int64  `json:"derivation_index"`
}

// signPageJSON is what gets embedded into consolidation_sign.html as
// {type="application/json"} text — never as template.JS/inline <script>
// (CLAUDE.md安全鐵律3). All fields here are non-secret (public addresses,
// xpub, IDs) — the browser still requires the operator's own mnemonic/
// private key input to do anything with them.
type signPageJSON struct {
	Type                  string     `json:"type"` // "consolidation" | "fee-topup"
	BatchID               int64      `json:"batch_id"`
	MasterWalletID        int64      `json:"master_wallet_id"`
	Xpub                  string     `json:"xpub,omitempty"`                     // consolidation only
	DestinationAddress    string     `json:"destination_address,omitempty"`      // consolidation only
	FeeSource             string     `json:"fee_source,omitempty"`               // fee-topup only
	FeeSourceAddress      string     `json:"fee_source_address,omitempty"`       // fee-topup only
	DefaultAmountPerOrder int64      `json:"default_amount_per_order,omitempty"` // fee-topup only, prefill
	Items                 []signItem `json:"items"`
}

type signPageData struct {
	Type         string
	PageDataJSON string // pre-marshaled JSON text, embedded via ordinary (non-JS-context) auto-escaping — see templates/consolidation_sign.html
}

var errInvalidSignType = errors.New("consolidation: type must be consolidation or fee-topup")

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
		if kind != "consolidation" && kind != "fee-topup" {
			http.Error(w, errInvalidSignType.Error(), http.StatusBadRequest)
			return
		}

		batchID, err := strconv.ParseInt(r.URL.Query().Get("batch_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid batch_id", http.StatusBadRequest)
			return
		}

		var masterWalletID int64
		page := signPageJSON{Type: kind, BatchID: batchID}

		switch kind {
		case "consolidation":
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
			page.FeeSourceAddress = batch.FeeSourceAddress

			if amt, err := strconv.ParseInt(r.URL.Query().Get("amount_per_order"), 10, 64); err == nil && amt > 0 {
				page.DefaultAmountPerOrder = amt
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
			page.Items = append(page.Items, signItem{
				OrderID:         ord.ID,
				Address:         ord.Address,
				DerivationIndex: ord.DerivationIndex,
			})
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
