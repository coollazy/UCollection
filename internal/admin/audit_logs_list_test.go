package admin

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/audit"
	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestAuditLogsListHandler_FiltersAndShowsDetail(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	targetType := "order"
	targetID := int64(7)
	if err := audit.Log(t.Context(), pool, "admin", "ORDER_MANUAL_CREATED", &targetType, &targetID, map[string]any{"note": "hello"}); err != nil {
		t.Fatalf("audit.Log() error = %v", err)
	}
	if err := audit.Log(t.Context(), pool, "admin", "LOGIN_SUCCESS", nil, nil, nil); err != nil {
		t.Fatalf("audit.Log() error = %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/audit-logs?action_type=ORDER_MANUAL_CREATED", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "note=hello") {
		t.Errorf("body missing expanded detail, got: %s", text)
	}
	if strings.Contains(text, "LOGIN_SUCCESS") {
		t.Errorf("filtered-out entry should not appear, got: %s", text)
	}
	if !strings.Contains(text, "共 1 筆") {
		t.Errorf("body missing correct total count, got: %s", text)
	}
}
