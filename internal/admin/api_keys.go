package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

type apiKeyRow struct {
	ID        int64
	CreatedAt time.Time
	Revoked   bool
}

type apiKeysListPageData struct {
	Keys       []apiKeyRow
	FlashError string
}

// apiKeysListHandler implements GET /admin/api-keys (技術架構設計第11節「API Key
// 管理」列表：不明碼顯示key/secret，僅顯示建立時間、狀態).
func apiKeysListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := deps.Pool.Query(r.Context(), `
			SELECT id, created_at, revoked_at IS NOT NULL FROM api_keys ORDER BY created_at DESC
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var keys []apiKeyRow
		for rows.Next() {
			var k apiKeyRow
			if err := rows.Scan(&k.ID, &k.CreatedAt, &k.Revoked); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "api_keys.html", apiKeysListPageData{Keys: keys})
	}
}

type apiKeyCreatedPageData struct {
	Key    string
	Secret string
}

// regenerateAPIKeyHandler implements POST /admin/api-keys/regenerate (技術架構
// 設計第11節「API Key 管理」：單一使用中API Key，同一transaction內把既有記錄
// revoked_at=now()、新增一筆記錄。key/secret僅於本次HTTP回應中明碼回傳一次).
// key_hash uses hex(sha256(key)) — must match internal/api/auth.go's
// validation exactly (see that file's authenticate()).
func regenerateAPIKeyHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		key, err := randomAPIKeyMaterial()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		secret, err := randomAPIKeyMaterial()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		sum := sha256.Sum256([]byte(key))
		keyHash := hex.EncodeToString(sum[:])

		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE revoked_at IS NULL`); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var newID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_keys (key_hash, secret) VALUES ($1, $2) RETURNING id
		`, keyHash, secret).Scan(&newID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		targetType := "api_key"
		_ = audit.Log(ctx, deps.Pool, "admin", "API_KEY_REGENERATED", &targetType, &newID, nil)

		render(w, http.StatusOK, "api_key_created.html", apiKeyCreatedPageData{Key: key, Secret: secret})
	}
}

// randomAPIKeyMaterial follows the same 32-byte crypto/rand +
// base64.RawURLEncoding shape internal/auth/session.go and
// internal/order/order.go already use for their own tokens — this project
// has no shared random-token helper package, each caller rolls its own copy.
func randomAPIKeyMaterial() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
