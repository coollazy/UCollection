package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/coollazy/UCollection/internal/audit"
)

type apiKeyRow struct {
	ID         int64
	CreatedAt  time.Time
	Revoked    bool
	KeyHint    sql.NullString
	SecretHint string
}

type apiKeysListPageData struct {
	Keys       []apiKeyRow
	FlashError string
	Notice     string
}

// apiKeysListHandler implements GET /admin/api-keys (技術架構設計第11節「API Key
// 管理」列表：不常態明碼顯示key/secret，僅顯示建立時間、狀態、前4+後4碼識別片段
// （key_hint/secret的hint現算，見docs/adr/0019）).
func apiKeysListHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := deps.Pool.Query(r.Context(), `
			SELECT id, created_at, revoked_at IS NOT NULL, key_hint, secret FROM api_keys ORDER BY created_at DESC
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var keys []apiKeyRow
		for rows.Next() {
			var k apiKeyRow
			var secret string
			if err := rows.Scan(&k.ID, &k.CreatedAt, &k.Revoked, &k.KeyHint, &secret); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			k.SecretHint = keyHint(secret)
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		render(w, http.StatusOK, "api_keys.html", apiKeysListPageData{Keys: keys, Notice: totpReverifiedNotice(r)})
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
			INSERT INTO api_keys (key_hash, key_hint, secret) VALUES ($1, $2, $3) RETURNING id
		`, keyHash, keyHint(key), secret).Scan(&newID); err != nil {
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

// keyHint returns a "front4...back4" identification snippet for API
// key/secret material — plaintext, but only 8 of the 43 random characters,
// not enough to reconstruct the full value (見 docs/adr/0019、CLAUDE.md安全
// 鐵律8). Used both for the stored api_keys.key_hint column (key's plaintext
// is unrecoverable after creation) and computed on the fly for api_keys.secret
// / system_params.webhook_secret (already stored plaintext per ADR-0010, so
// no extra column needed there). Webhook secret allows manual input and may
// be shorter than 8 chars — returned as-is in that case, which reveals no
// more than what's already sitting in the DB in plaintext.
func keyHint(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:4] + "..." + s[len(s)-4:]
}
