package admin

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/tronclient"
)

func TestMasterWalletsListHandler(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	if _, err := pool.Exec(t.Context(), `UPDATE master_wallets SET last_derived_index = 7 WHERE id = $1`, walletID); err != nil {
		t.Fatalf("set last_derived_index: %v", err)
	}

	deps := Deps{Pool: pool, TronClient: tronclient.NewClient("http://unused.invalid", ""), USDTContractAddress: "T-unused"}
	srv := newTestServer(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/admin/master-wallets", nil)
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

	wantTail := xpubTail(testXpub)
	if !strings.Contains(text, wantTail) {
		t.Errorf("body missing masked xpub %q, got: %s", wantTail, text)
	}
	if strings.Contains(text, testXpub) {
		t.Error("body must not contain the full xpub, only the masked tail")
	}
	if !strings.Contains(text, "使用中") {
		t.Error("body missing active-status label")
	}
	if !strings.Contains(text, "7") {
		t.Error("body missing last_derived_index")
	}
}

func TestXpubTail(t *testing.T) {
	if got := xpubTail("xpub6D1AabNHCupeiLM65ZR9UStM"); got != "...UStM" {
		t.Errorf("xpubTail() = %q, want ...UStM", got)
	}
	if got := xpubTail("abc"); got != "abc" {
		t.Errorf("xpubTail() short input = %q, want unchanged abc", got)
	}
}
