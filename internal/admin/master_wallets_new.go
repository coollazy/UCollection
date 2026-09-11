package admin

import (
	"encoding/json"
	"net/http"

	"github.com/coollazy/UCollection/internal/audit"
)

// masterWalletNewCSPHeader duplicates internal/consolidation's
// signCSPHeader value verbatim (驗證結論-08 已驗證的嚴格CSP值，CLAUDE.md安全鐵律3：
// 處理助記詞/私鑰輸入的頁面套用嚴格CSP). It's a separate constant, not an
// import, because internal/consolidation's is unexported and this project's
// established convention is that packages don't share a common owner for
// small cross-cutting constants (見internal/consolidation/masterwallet.go
// 對master_wallets表本身的同一句話). connect-src 'self' stays because this
// page submits xpub via fetch(), not a native <form> — form-action 'none'
// is therefore also left untouched (no form submission ever happens here).
const masterWalletNewCSPHeader = "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// masterWalletNewPageHandler implements GET /admin/master-wallets/new
// (技術架構設計第11節「新增」). All key derivation happens client-side in the
// offline-bundled JS (CLAUDE.md安全鐵律1) — this handler has nothing to
// query, it only needs to apply the strict CSP header before rendering.
func masterWalletNewPageHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", masterWalletNewCSPHeader)
		render(w, http.StatusOK, "master_wallet_new.html", nil)
	}
}

type createMasterWalletRequest struct {
	Xpub string `json:"xpub"`
}

type createMasterWalletResponse struct {
	OK       bool   `json:"ok"`
	Redirect string `json:"redirect,omitempty"`
	Error    string `json:"error,omitempty"`
}

// maxXpubLen is a generous sanity bound, not a format validator — the
// browser-side BIP32 library is what actually produced this string
// correctly or not; the backend only needs to reject obviously-wrong
// payloads (empty, or absurdly large) before it ever touches the DB.
const maxXpubLen = 512

// createMasterWalletHandler implements POST /admin/master-wallets (技術架構設計
// 第11節「新增」: 後端收到xpub後比對重複、不重複則新增一筆status='active'、
// last_derived_index=0，同一transaction內把原本使用中的那筆改為inactive). The
// browser sends xpub as JSON (fetch, not a native form — see
// masterWalletNewCSPHeader's comment on why), so this responds with JSON
// too rather than a 303 redirect.
func createMasterWalletHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createMasterWalletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, createMasterWalletResponse{OK: false, Error: "bad_request"})
			return
		}
		if req.Xpub == "" || len(req.Xpub) > maxXpubLen {
			writeJSON(w, http.StatusBadRequest, createMasterWalletResponse{OK: false, Error: "bad_request"})
			return
		}

		ctx := r.Context()
		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, createMasterWalletResponse{OK: false, Error: "internal_error"})
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM master_wallets WHERE xpub = $1)`, req.Xpub).Scan(&exists); err != nil {
			writeJSON(w, http.StatusInternalServerError, createMasterWalletResponse{OK: false, Error: "internal_error"})
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, createMasterWalletResponse{OK: false, Error: "duplicate"})
			return
		}

		if _, err := tx.Exec(ctx, `UPDATE master_wallets SET status = 'inactive' WHERE status = 'active'`); err != nil {
			writeJSON(w, http.StatusInternalServerError, createMasterWalletResponse{OK: false, Error: "internal_error"})
			return
		}
		var newID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO master_wallets (xpub, status, last_derived_index) VALUES ($1, 'active', 0) RETURNING id
		`, req.Xpub).Scan(&newID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, createMasterWalletResponse{OK: false, Error: "internal_error"})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			writeJSON(w, http.StatusInternalServerError, createMasterWalletResponse{OK: false, Error: "internal_error"})
			return
		}

		targetType := "master_wallet"
		_ = audit.Log(ctx, deps.Pool, "admin", "MASTER_WALLET_CREATED", &targetType, &newID, nil)

		writeJSON(w, http.StatusOK, createMasterWalletResponse{OK: true, Redirect: "/admin/master-wallets"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
