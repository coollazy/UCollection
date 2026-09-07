package consolidation

import (
	"log"
	"net/http"
	"strconv"
)

type pendingWalletsPageData struct {
	Wallets []masterWalletOption
}

type pendingListPageData struct {
	MasterWalletID int64
	Entries        []PendingEntry
	Notice         string
}

// pendingListPageHandler implements GET /admin/consolidation (技術架構設計第
// 10節「待歸集地址列表」): resolves which master wallet to show (skipping
// the picker entirely when there's exactly one — 步驟1「若系統僅有一個使用中/
// 曾用過的代收主錢包可省略此步驟」), triggers the lazy reconcile pass before
// rendering (廣播結果判斷: catch up any items still stuck in 'broadcasting'
// since the last page load), then lists candidates with a positive live
// balance.
func pendingListPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		idParam := r.URL.Query().Get("master_wallet_id")
		if idParam == "" {
			wallets, err := listMasterWallets(ctx, deps.Pool)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			switch len(wallets) {
			case 0:
				render(w, http.StatusOK, "consolidation_wallets.html", pendingWalletsPageData{})
				return
			case 1:
				idParam = strconv.FormatInt(wallets[0].ID, 10)
			default:
				// No master_wallet_id selected yet and more than one exists —
				// reconcile across all of them (技術架構設計第10節「尚未選定代收
				// 主錢包時的查詢範圍」) before showing the picker, so a stale
				// 'broadcasting' item doesn't linger just because the operator
				// hasn't picked a wallet yet.
				if err := ReconcileBroadcasting(ctx, deps, nil); err != nil {
					log.Printf("consolidation: reconcile (all wallets) on page load: %v", err)
				}
				render(w, http.StatusOK, "consolidation_wallets.html", pendingWalletsPageData{Wallets: wallets})
				return
			}
		}

		masterWalletID, err := strconv.ParseInt(idParam, 10, 64)
		if err != nil {
			http.Error(w, "invalid master_wallet_id", http.StatusBadRequest)
			return
		}

		if err := ReconcileBroadcasting(ctx, deps, &masterWalletID); err != nil {
			// Reconcile is background correction, not a precondition for
			// rendering the list correctly — the list itself always reflects
			// live on-chain balances regardless (技術架構設計第10節), so a
			// reconcile failure just means we log it and try again next load.
			log.Printf("consolidation: reconcile on page load: %v", err)
		}

		entries, err := ListPendingConsolidation(ctx, deps, masterWalletID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "consolidation_pending.html", pendingListPageData{MasterWalletID: masterWalletID, Entries: entries, Notice: totpReverifiedNotice(r)})
	}
}
