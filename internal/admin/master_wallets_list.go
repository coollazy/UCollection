package admin

import (
	"net/http"
	"time"
)

// masterWalletRow is what the list page shows — xpub is masked to its last
// 4 chars (same truncation convention web/js-src/page.js:244 already uses
// for the operator-facing IndexedDB label, "xpub末4碼").
type masterWalletRow struct {
	ID               int64
	XpubTail         string
	Status           string
	LastDerivedIndex int64
	CreatedAt        time.Time
}

type masterWalletsListPageData struct {
	Wallets    []masterWalletRow
	FlashError string
}

// masterWalletsListHandler implements GET /admin/master-wallets (技術架構設計
// 第11節「代收主錢包設定」列表：xpub僅顯示末幾碼＋建立日期、狀態、已分配索引數).
func masterWalletsListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := deps.Pool.Query(r.Context(), `
			SELECT id, xpub, status, last_derived_index, created_at
			FROM master_wallets
			ORDER BY created_at DESC
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var wallets []masterWalletRow
		for rows.Next() {
			var (
				id               int64
				xpub, status     string
				lastDerivedIndex int64
				createdAt        time.Time
			)
			if err := rows.Scan(&id, &xpub, &status, &lastDerivedIndex, &createdAt); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			wallets = append(wallets, masterWalletRow{
				ID:               id,
				XpubTail:         xpubTail(xpub),
				Status:           status,
				LastDerivedIndex: lastDerivedIndex,
				CreatedAt:        createdAt,
			})
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "master_wallets.html", masterWalletsListPageData{
			Wallets:    wallets,
			FlashError: masterWalletsReactivateErrorMessage(r.URL.Query().Get("error")),
		})
	}
}

func xpubTail(xpub string) string {
	const tailLen = 4
	if len(xpub) <= tailLen {
		return xpub
	}
	return "..." + xpub[len(xpub)-tailLen:]
}
