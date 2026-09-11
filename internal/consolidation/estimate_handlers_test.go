package consolidation

import (
	"net/http"
	"testing"
)

func TestEnergyToTRXSun_RoundsUpWithBuffer(t *testing.T) {
	tests := []struct {
		name       string
		energyUsed int64
		energyFee  int64
		want       int64
	}{
		// 13279 * 100 * 11 / 10 = 1460690 -> rounds up to the next 100000 (0.1
		// TRX) multiple, 1500000 (1.5 TRX). Cross-checked against
		// TriggerConstantContract's real Shasta fixture (見internal/tronclient/
		// estimate_test.go的triggerConstantContractSuccessFixture).
		{"real fixture energy/fee", 13279, 100, 1_500_000},
		// 821 * 100 * 11 / 10 = 9031 -> rounds up to 100000 (0.1 TRX minimum).
		{"small energy rounds up to minimum unit", 821, 100, 100_000},
		// 1000 * 100 * 11 / 10 = 110000, just over one 100000 unit -> rounds up
		// to the next unit, 200000 (not 110000).
		{"just over one unit rounds up to the next unit", 1000, 100, 200_000},
		// Chosen so the buffered value lands exactly on a 100000 multiple:
		// 10000 * 100 * 11 / 10 = 1100000, already an exact multiple -> no
		// extra rounding beyond the buffer itself.
		{"exact multiple after buffer needs no extra rounding", 10000, 100, 1_100_000},
		{"zero energy is zero fee", 0, 100, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := energyToTRXSun(tc.energyUsed, tc.energyFee)
			if got != tc.want {
				t.Errorf("energyToTRXSun(%d, %d) = %d, want %d", tc.energyUsed, tc.energyFee, got, tc.want)
			}
		})
	}
}

// getChainParametersFixture returns a getchainparameters response carrying
// only the one key feeEstimateHandler reads (real shape confirmed in
// internal/tronclient/estimate_test.go).
func getChainParametersFixture(energyFee int64) string {
	return `{"chainParameter":[{"key":"getEnergyFee","value":` + itoa(energyFee) + `}]}`
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestFeeEstimateHandler_WithDestination(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "fee-estimate-with-dest")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getchainparameters", http.StatusOK, getChainParametersFixture(100))
	// repeatEnergy (owner->owner)
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, triggerConstantContractFixture(13279, false, ""))
	// firstEnergy (owner->destination)
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, triggerConstantContractFixture(821, false, ""))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id":    walletID,
		"destination_address": testDestinationAddress,
		"order_ids":           []int64{o.ID},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[feeEstimateResponse](t, resp)

	if got.RepeatEnergy != 13279 || got.FirstEnergy != 821 {
		t.Fatalf("got energies (repeat=%d, first=%d), want (repeat=13279, first=821)", got.RepeatEnergy, got.FirstEnergy)
	}
	if got.EnergyFee != 100 {
		t.Fatalf("EnergyFee = %d, want 100", got.EnergyFee)
	}
	if got.RepeatTRXSun != 1_500_000 {
		t.Errorf("RepeatTRXSun = %d, want 1500000 (verified against energyToTRXSun(13279,100))", got.RepeatTRXSun)
	}
	if got.FirstTRXSun != 100_000 {
		t.Errorf("FirstTRXSun = %d, want 100000 (verified against energyToTRXSun(821,100))", got.FirstTRXSun)
	}
	if got.RepeatTRX != "1.5" {
		t.Errorf("RepeatTRX = %q, want %q", got.RepeatTRX, "1.5")
	}
	if got.FirstTRX != "0.1" {
		t.Errorf("FirstTRX = %q, want %q", got.FirstTRX, "0.1")
	}
}

func TestFeeEstimateHandler_WithoutDestination_DerivesProbeAddress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "fee-estimate-no-dest")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getchainparameters", http.StatusOK, getChainParametersFixture(100))
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, triggerConstantContractFixture(13279, false, ""))
	// firstEnergy simulated against the derived probe address instead of an
	// operator-picked destination — same TronGrid call shape either way, this
	// fixture just represents "brand new recipient" costing more energy.
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, triggerConstantContractFixture(65566, false, ""))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id": walletID,
		"order_ids":        []int64{o.ID},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[feeEstimateResponse](t, resp)
	if got.FirstEnergy != 65566 {
		t.Errorf("FirstEnergy = %d, want 65566 (from the probe-address simulation)", got.FirstEnergy)
	}
	if got.RepeatEnergy != 13279 {
		t.Errorf("RepeatEnergy = %d, want 13279", got.RepeatEnergy)
	}
}

func TestFeeEstimateHandler_RepeatReverted(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "fee-estimate-reverted")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getchainparameters", http.StatusOK, getChainParametersFixture(100))
	mock.enqueue("/wallet/triggerconstantcontract", http.StatusOK, triggerConstantContractFixture(821, true, "REVERT opcode executed"))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id":    walletID,
		"destination_address": testDestinationAddress,
		"order_ids":           []int64{o.ID},
	})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the repeat-energy simulation reverts", resp.StatusCode)
	}
}

func TestFeeEstimateHandler_OrderWalletMismatch(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)
	otherWalletID := newMasterWallet(t, pool)
	o := newOrder(t, pool, walletID, "fee-estimate-mismatch")

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getchainparameters", http.StatusOK, getChainParametersFixture(100))
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id":    otherWalletID,
		"destination_address": testDestinationAddress,
		"order_ids":           []int64{o.ID},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when order belongs to a different master wallet", resp.StatusCode)
	}
}

func TestFeeEstimateHandler_EmptyOrderIDs(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	walletID := newMasterWallet(t, pool)

	mock := newMockTronGrid() // no TronGrid call expected — request is rejected before any lookup
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id": walletID,
		"order_ids":        []int64{},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an empty order_ids list", resp.StatusCode)
	}
}

func TestFeeEstimateHandler_RequireSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)

	resp := postJSON(t, srv, nil, "", "/admin/consolidation/fee-estimate", map[string]any{
		"master_wallet_id": 1, "order_ids": []int64{1},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login", resp.StatusCode)
	}
}

// triggerConstantContractFixture builds a triggerconstantcontract response
// mirroring the real shapes captured in internal/tronclient/estimate_test.go
// (result.result=true even on revert; the revert only shows in message).
func triggerConstantContractFixture(energyUsed int64, reverted bool, message string) string {
	if reverted {
		return `{"result":{"result":true,"message":"` + message + `"},"energy_used":` + itoa(energyUsed) + `,"constant_result":["00"]}`
	}
	return `{"result":{"result":true},"energy_used":` + itoa(energyUsed) + `,"constant_result":["00"]}`
}

func TestTRXBalanceHandler(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getaccount", http.StatusOK, `{"balance":15695500}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/trx-balance", map[string]any{
		"address": testSourceAddress,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[trxBalanceResponse](t, resp)
	if got.BalanceSun != 15695500 {
		t.Errorf("BalanceSun = %d, want 15695500", got.BalanceSun)
	}
	if got.BalanceTRX != "15.6955" {
		t.Errorf("BalanceTRX = %q, want %q", got.BalanceTRX, "15.6955")
	}
}

func TestTRXBalanceHandler_UnactivatedAccount(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	mock.enqueue("/wallet/getaccount", http.StatusOK, `{}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/trx-balance", map[string]any{
		"address": testSourceAddress,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[trxBalanceResponse](t, resp)
	if got.BalanceSun != 0 || got.BalanceTRX != "0" {
		t.Errorf("got = %+v, want zero balance for an unactivated account", got)
	}
}

func TestTRXBalanceHandler_InvalidAddress(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid() // no TronGrid call expected — invalid address rejected before any lookup
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/admin/consolidation/trx-balance", map[string]any{
		"address": "not-a-tron-address",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTransactionInfoHandler_USDTSuccess(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-usdt-ok","blockNumber":100,"receipt":{"result":"SUCCESS"}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-usdt-ok", "kind": "usdt",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[transactionInfoResponse](t, resp)
	if !got.Found || !got.Success {
		t.Errorf("got = %+v, want Found=true Success=true", got)
	}
}

func TestTransactionInfoHandler_USDTRevert(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-usdt-revert","blockNumber":100,"receipt":{"result":"REVERT"}}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-usdt-revert", "kind": "usdt",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[transactionInfoResponse](t, resp)
	if !got.Found || got.Success {
		t.Errorf("got = %+v, want Found=true Success=false for a REVERT receipt", got)
	}
}

func TestTransactionInfoHandler_TRXSuccess(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	// A native TransferContract's gettransactioninfobyid response has no
	// receipt.result field at all (驗證結論-06 附加測試) — success for kind=trx
	// must come from blockNumber alone.
	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{"id":"tx-trx-ok","blockNumber":50}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-trx-ok", "kind": "trx",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[transactionInfoResponse](t, resp)
	if !got.Found || !got.Success {
		t.Errorf("got = %+v, want Found=true Success=true", got)
	}
}

func TestTransactionInfoHandler_NotFound(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid()
	mock.enqueue("/wallet/gettransactioninfobyid", http.StatusOK, `{}`)
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-unknown", "kind": "usdt",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, readBody(t, resp))
	}
	got := decodeJSON[transactionInfoResponse](t, resp)
	if got.Found || got.Success {
		t.Errorf("got = %+v, want Found=false Success=false when TronGrid doesn't know this tx yet", got)
	}
}

func TestTransactionInfoHandler_InvalidKind(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)

	mock := newMockTronGrid() // no TronGrid call expected — invalid kind rejected before any lookup
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)
	cookie := newActiveSessionCookie(t, pool)

	resp := postJSON(t, srv, cookie, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-x", "kind": "bogus",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTransactionInfoHandler_RequireSession(t *testing.T) {
	pool := testPool(t)
	resetDB(t, pool)
	mock := newMockTronGrid()
	tc := mock.start(t)
	deps := Deps{Pool: pool, TronClient: tc, USDTContractAddress: testUSDTContract}
	srv := newTestMux(t, deps)

	resp := postJSON(t, srv, nil, "", "/tron-proxy/consolidation/transaction-info", map[string]any{
		"tx_id": "tx-x", "kind": "usdt",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login", resp.StatusCode)
	}
}
