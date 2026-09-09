package tronclient

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Fixtures below are real responses captured against the live Shasta
// testnet during this module's development (see docs/進度.md 驗證測試
// 2026-09-09 條目8) — not hand-assembled guesses.

// triggerConstantContractBalanceFixture is a real balanceOf(address) result
// for TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm, an address that had received 50
// USDT but had never had any other on-chain activity (never received TRX).
// GET /v1/accounts/{address} returns an empty account for this exact
// address (confirmed against the live testnet) — this is the fixture that
// demonstrates TRC20Balance no longer depends on that indexed endpoint.
const triggerConstantContractBalanceFixture = `{"transaction":{"raw_data":{"ref_block_bytes":"115f","ref_block_hash":"101784ed70bf1bc6","expiration":1788936951000,"contract":[{"parameter":{"value":{"owner_address":"TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm","contract_address":"TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs","data":"70a08231000000000000000000000000692b38979ed5e6a992732d7cf865e02aa23a50f7"},"type_url":"type.googleapis.com/protocol.TriggerSmartContract"},"type":"TriggerSmartContract"}],"timestamp":1788936892669},"ret":[{}],"raw_data_hex":"0a02115f2208101784ed70bf1bc640d8a9cea788345a8e01081f1289010a31747970652e676f6f676c65617069732e636f6d2f70726f746f636f6c2e54726967676572536d617274436f6e747261637412540a1541692b38979ed5e6a992732d7cf865e02aa23a50f712154142a1e39aefa49290f2b3f9ed688d7cecf86cd6e0222470a08231000000000000000000000000692b38979ed5e6a992732d7cf865e02aa23a50f770fde1caa78834","txID":"7101dfccf9c91dba727ac6a6f60314b2da96f4f2e2d3e2d2c83d949f162d7fdf","visible":true},"constant_result":["0000000000000000000000000000000000000000000000000000000002faf080"],"result":{"result":true},"energy_used":541}`

// triggerConstantContractZeroBalanceFixture is a real balanceOf(address)
// result for a freshly-generated, never-used Tron address.
const triggerConstantContractZeroBalanceFixture = `{"transaction":{"raw_data":{"ref_block_bytes":"12b0","ref_block_hash":"f7e0d662e133ee0c","expiration":1788937974000,"contract":[{"parameter":{"value":{"owner_address":"TEhkcpWhwioYBVu9bz6DPtmcz53mbkLbFn","contract_address":"TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs","data":"70a0823100000000000000000000000033ec5d9f5b0f59f986079482000540ef453468de"},"type_url":"type.googleapis.com/protocol.TriggerSmartContract"},"type":"TriggerSmartContract"}],"timestamp":1788937915868},"ret":[{}],"raw_data_hex":"0a0212b02208f7e0d662e133ee0c40f0e18ca888345a8e01081f1289010a31747970652e676f6f676c65617069732e636f6d2f70726f746f636f6c2e54726967676572536d617274436f6e747261637412540a154133ec5d9f5b0f59f986079482000540ef453468de12154142a1e39aefa49290f2b3f9ed688d7cecf86cd6e0222470a0823100000000000000000000000033ec5d9f5b0f59f986079482000540ef453468de70dc9b89a88834","txID":"8c7a0d7d09ecffa803f1715638e582606670bdd1afe8facdf78573fbefed5233","visible":true},"constant_result":["0000000000000000000000000000000000000000000000000000000000000000"],"result":{"result":true},"energy_used":541}`

func TestTRC20Balance_UnactivatedAddressWithBalance(t *testing.T) {
	// This is the regression case for the bug this rewrite fixes: an
	// address with a real USDT balance but no other on-chain activity,
	// which GET /v1/accounts/{address} cannot see at all.
	srv := jsonServer(t, http.StatusOK, triggerConstantContractBalanceFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TRC20Balance(context.Background(), "TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err != nil {
		t.Fatalf("TRC20Balance() error = %v", err)
	}
	if got != 50000000 {
		t.Errorf("TRC20Balance() = %d, want 50000000", got)
	}
}

func TestTRC20Balance_ZeroBalance(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerConstantContractZeroBalanceFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TRC20Balance(context.Background(), "TEhkcpWhwioYBVu9bz6DPtmcz53mbkLbFn", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err != nil {
		t.Fatalf("TRC20Balance() error = %v", err)
	}
	if got != 0 {
		t.Errorf("TRC20Balance() = %d, want 0", got)
	}
}

func TestTRC20Balance_InvalidAddress(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerConstantContractZeroBalanceFixture)
	c := NewClient(srv.URL, "")

	_, err := c.TRC20Balance(context.Background(), "not-a-tron-address", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err == nil {
		t.Fatal("TRC20Balance() error = nil, want error for malformed address")
	}
}

func TestTRC20Balance_ContractCallError(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerSmartContractErrorFixture)
	c := NewClient(srv.URL, "")

	_, err := c.TRC20Balance(context.Background(), "TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err == nil {
		t.Fatal("TRC20Balance() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "OTHER_ERROR") {
		t.Errorf("error = %v, want it to include the TronGrid error code", err)
	}
}
