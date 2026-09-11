package tronclient

import (
	"context"
	"net/http"
	"testing"
)

// Fixtures below are real responses captured against the live Shasta
// testnet during this module's development — not hand-assembled guesses.

// triggerConstantContractSuccessFixture is a real triggerconstantcontract
// result for a normal transfer() simulation with no revert.
const triggerConstantContractSuccessFixture = `{"result":{"result":true},"energy_used":13279,"constant_result":["0000000000000000000000000000000000000000000000000000000000000001"],"transaction":{"visible":true}}`

// triggerConstantContractRevertFixture is a real triggerconstantcontract
// result for a transfer() simulation where the owner has no token balance.
// Note result.result=true even though the call actually reverted — the
// revert only shows up in result.message ("REVERT opcode executed").
const triggerConstantContractRevertFixture = `{"constant_result":["0000000000000000000000000000000000000000000000000000000000000000"],"energy_used":821,"result":{"result":true,"message":"REVERT opcode executed"},"transaction":{"visible":true}}`

func TestTriggerConstantContract_Success(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerConstantContractSuccessFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TriggerConstantContract(context.Background(), TriggerSmartContractParams{
		OwnerAddress:     "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH",
		ContractAddress:  "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs",
		FunctionSelector: "transfer(address,uint256)",
		Parameter:        "000000000000000000000000b6e708a39781c96bd399c7657780ff9fe9f052a8000000000000000000000000000000000000000000000000000000000012d687",
	})
	if err != nil {
		t.Fatalf("TriggerConstantContract() error = %v", err)
	}
	if got.Reverted {
		t.Error("Reverted = true, want false for a normal successful call")
	}
	if got.EnergyUsed != 13279 {
		t.Errorf("EnergyUsed = %d, want 13279", got.EnergyUsed)
	}
	if len(got.ConstantResult) != 1 {
		t.Fatalf("ConstantResult = %v, want 1 entry", got.ConstantResult)
	}
}

func TestTriggerConstantContract_Revert(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, triggerConstantContractRevertFixture)
	c := NewClient(srv.URL, "")

	got, err := c.TriggerConstantContract(context.Background(), TriggerSmartContractParams{
		OwnerAddress:     "TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm",
		ContractAddress:  "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs",
		FunctionSelector: "transfer(address,uint256)",
	})
	if err != nil {
		t.Fatalf("TriggerConstantContract() error = %v, want nil (this is a business-level revert, not a transport error)", err)
	}
	if !got.Reverted {
		t.Error("Reverted = false, want true even though result.result=true, because message contains REVERT")
	}
	if got.EnergyUsed != 821 {
		t.Errorf("EnergyUsed = %d, want 821", got.EnergyUsed)
	}
	if got.Message == "" {
		t.Error("Message = \"\", want the REVERT message")
	}
}

func TestGetChainParameters(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{"chainParameter":[{"key":"getEnergyFee","value":100},{"key":"getTotalEnergyLimit","value":180000000000},{"key":"getAllowSameTokenName"}]}`)
	c := NewClient(srv.URL, "")

	got, err := c.GetChainParameters(context.Background())
	if err != nil {
		t.Fatalf("GetChainParameters() error = %v", err)
	}
	if got["getEnergyFee"] != 100 {
		t.Errorf("getEnergyFee = %d, want 100", got["getEnergyFee"])
	}
	if got["getTotalEnergyLimit"] != 180000000000 {
		t.Errorf("getTotalEnergyLimit = %d, want 180000000000", got["getTotalEnergyLimit"])
	}
	if v, ok := got["getAllowSameTokenName"]; !ok || v != 0 {
		t.Errorf("getAllowSameTokenName = %d (present=%v), want 0 for a key that omits \"value\"", v, ok)
	}
}

func TestAccountTRXBalance_Activated(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{"balance":15695500,"address":"41692b38979ed5e6a992732d7cf865e02aa23a50f7","account_resource":{}}`)
	c := NewClient(srv.URL, "")

	got, err := c.AccountTRXBalance(context.Background(), "TKZHjFeUUFcr51y7k1oS5sc2BgmHLTT4hm")
	if err != nil {
		t.Fatalf("AccountTRXBalance() error = %v", err)
	}
	if got != 15695500 {
		t.Errorf("AccountTRXBalance() = %d, want 15695500", got)
	}
}

func TestAccountTRXBalance_UnactivatedAccount(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{}`)
	c := NewClient(srv.URL, "")

	got, err := c.AccountTRXBalance(context.Background(), "TEhkcpWhwioYBVu9bz6DPtmcz53mbkLbFn")
	if err != nil {
		t.Fatalf("AccountTRXBalance() error = %v", err)
	}
	if got != 0 {
		t.Errorf("AccountTRXBalance() = %d, want 0 for an unactivated account", got)
	}
}
