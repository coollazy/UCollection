package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coollazy/UCollection/internal/api"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestRegenerateAPIKeyHandler_Success(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	oldID := newAPIKey(t, pool, "old-key", "old-secret")

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/admin/api-keys/regenerate", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var revokedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT revoked_at FROM api_keys WHERE id = $1`, oldID).Scan(&revokedAt); err != nil {
		t.Fatalf("query old key: %v", err)
	}
	if revokedAt == nil {
		t.Error("old key revoked_at is nil, want set")
	}

	var newKeyCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM api_keys WHERE revoked_at IS NULL`).Scan(&newKeyCount); err != nil {
		t.Fatalf("count active keys: %v", err)
	}
	if newKeyCount != 1 {
		t.Errorf("active api_keys count = %d, want 1", newKeyCount)
	}

	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action_type = 'API_KEY_REGENERATED'`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_logs: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("API_KEY_REGENERATED audit_logs count = %d, want 1", auditCount)
	}
}

// TestRegenerateAPIKeyHandler_NewKeyValidatesAgainstAPIPackage proves the
// newly regenerated key/secret actually work against internal/api's own
// HMAC validation (internal/api/auth.go's authenticate()), not just that a
// row landed in the DB with the right shape. The raw key/secret are only
// ever shown once in api_key_created.html's response body — that's the
// only place a test (or a real operator) can read them from.
func TestRegenerateAPIKeyHandler_NewKeyValidatesAgainstAPIPackage(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	adminSrv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodPost, adminSrv.URL+"/admin/api-keys/regenerate", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	rawKey := betweenTags(t, string(body), "Key: <code>", "</code>")
	rawSecret := betweenTags(t, string(body), "Secret: <code>", "</code>")

	var dbSecret string
	if err := pool.QueryRow(context.Background(), `SELECT secret FROM api_keys WHERE revoked_at IS NULL`).Scan(&dbSecret); err != nil {
		t.Fatalf("query new key: %v", err)
	}
	if rawSecret != dbSecret {
		t.Fatalf("secret extracted from HTML (%q) does not match DB (%q)", rawSecret, dbSecret)
	}

	apiSrv := httptest.NewServer(api.NewMux(pool, "https://admin.example"))
	t.Cleanup(apiSrv.Close)

	method := http.MethodGet
	requestURI := "/api/v1/orders/1"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signed := timestamp + method + requestURI
	mac := hmac.New(sha256.New, []byte(rawSecret))
	mac.Write([]byte(signed))
	signature := hex.EncodeToString(mac.Sum(nil))

	apiReq, err := http.NewRequest(method, apiSrv.URL+requestURI, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	apiReq.Header.Set("X-API-Key", rawKey)
	apiReq.Header.Set("X-Timestamp", timestamp)
	apiReq.Header.Set("X-Signature", signature)
	apiResp, err := http.DefaultClient.Do(apiReq)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = apiResp.Body.Close() })

	var errBody struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.NewDecoder(apiResp.Body).Decode(&errBody)
	if apiResp.StatusCode == http.StatusUnauthorized || errBody.ErrorCode == "INVALID_SIGNATURE" {
		t.Fatalf("regenerated key failed HMAC auth against internal/api: status=%d body=%+v", apiResp.StatusCode, errBody)
	}
	if apiResp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 ORDER_NOT_FOUND (order 1 doesn't exist, but auth must have passed)", apiResp.StatusCode)
	}
}

func betweenTags(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("marker %q not found in: %s", start, s)
	}
	i += len(start)
	j := strings.Index(s[i:], end)
	if j < 0 {
		t.Fatalf("closing marker %q not found in: %s", end, s)
	}
	return s[i : i+j]
}
