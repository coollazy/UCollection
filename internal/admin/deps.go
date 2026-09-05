package admin

import (
	"net/http"

	"github.com/coollazy/UCollection/internal/scanner"
	"github.com/coollazy/UCollection/internal/store"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// Deps are the dependencies this package's HTTP handlers need.
type Deps struct {
	Pool                *store.Pool
	TronClient          *tronclient.Client
	USDTContractAddress string
	// HTTPClient is passed straight through to webhook.SendTestPing (POST
	// /admin/webhook-config/test) — nil is fine, SendTestPing falls back to
	// its own default client the same way webhook.Resend does.
	HTTPClient *http.Client
}

// scannerDeps adapts Deps to scanner.Deps for calling scanner.ReverifyOrder
// (技術架構設計第11節「手動重新檢查此地址」沿用第4節任務2/3同一組TronGrid依賴).
func (d Deps) scannerDeps() scanner.Deps {
	return scanner.Deps{Pool: d.Pool, TronClient: d.TronClient, ContractAddress: d.USDTContractAddress}
}
