package consolidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/coollazy/UCollection/internal/hdwallet"
	"github.com/coollazy/UCollection/internal/order"
	"github.com/coollazy/UCollection/internal/tronclient"
)

// consolidationProbeIndex is a fixed, far-beyond-any-real-order derivation
// index used only to produce a throwaway "definitely has never held this
// USDT contract" address for feeEstimateHandler's no-destination-picked
// branch — the caller hasn't decided where funds are going yet, so there is
// no real destination to simulate against. This has nothing to do with
// 安全鐵律5's reserved index 0 (that rule is about never assigning index 0 to
// a real order; this index is never assigned to anything, real or
// otherwise — it only ever exists as a throwaway read-only simulation
// target that no signing ever happens against).
const consolidationProbeIndex = 2_000_000_000

// feeEstimateRequest is POST /admin/consolidation/fee-estimate's request
// body. OrderIDs must contain at least one order id — only OrderIDs[0] is
// actually used (as the "owner" address the simulated transfer() is sent
// from); the full list is accepted so the caller can pass exactly what it
// already has selected without the frontend needing to special-case "just
// send me the first one".
type feeEstimateRequest struct {
	MasterWalletID     int64   `json:"master_wallet_id"`
	DestinationAddress string  `json:"destination_address"`
	OrderIDs           []int64 `json:"order_ids"`
}

// feeEstimateResponse carries only per-transfer figures for one "first"
// transfer and one "repeat" transfer — the caller (page.js) knows how many
// of the batch's selected items are first-time vs repeat and sums
// first_trx_sun + repeat_trx_sun*(n-1) itself; this is a 毛需求 estimate that
// does not subtract any address's existing TRX balance (that happens at
// actual top-up time, not here).
type feeEstimateResponse struct {
	FirstEnergy  int64  `json:"first_energy"`
	RepeatEnergy int64  `json:"repeat_energy"`
	EnergyFee    int64  `json:"energy_fee"`
	FirstTRXSun  int64  `json:"first_trx_sun"`
	RepeatTRXSun int64  `json:"repeat_trx_sun"`
	FirstTRX     string `json:"first_trx"`
	RepeatTRX    string `json:"repeat_trx"`
}

// energyToTRXSun converts a triggerconstantcontract energy_used figure into
// a TRX-sun fee estimate: +10% safety buffer, rounded UP to the nearest 0.1
// TRX (100000 sun). Pure int64 integer arithmetic throughout — CLAUDE.md
// 安全鐵律6 forbids float arithmetic on amounts, and this value ends up
// driving a real TRX top-up amount downstream, so it's in scope of that
// rule even though it's "just an estimate".
func energyToTRXSun(energyUsed, energyFee int64) int64 {
	const unit = 100_000
	raw := energyUsed * energyFee * 11 / 10
	return ((raw + unit - 1) / unit) * unit
}

// feeEstimateHandler implements POST /admin/consolidation/fee-estimate
// (手動歸集流程重構 Phase 2, see docs/進度.md): a read-only, no-TOTP energy/TRX
// cost estimate for a batch's transfers, computed via TronGrid's
// triggerconstantcontract simulation — never a local heuristic — so the
// operator can see roughly how much TRX to top up before actually preparing
// any real transaction.
//
// "First" vs "repeat" mirrors TRON's own first-time-recipient energy
// premium: a destination that has never held this USDT token costs more
// energy to receive into than one that already holds it. repeatEnergy
// always simulates owner->owner (the owner necessarily already holds USDT —
// it's a pending-consolidation candidate with balance>0), while firstEnergy
// simulates owner->destination (if the operator has picked one already) or
// owner->a fixed probe address that has certainly never held this USDT (if
// not). This "owner->self reads as repeat-recipient energy, owner->brand-
// new-address reads as first-recipient energy" assumption is not yet
// verified against a real chain — Phase 5's Shasta end-to-end pass is where
// that gets confirmed; this phase only tests the arithmetic and wiring
// against a mocked TronGrid.
func feeEstimateHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req feeEstimateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		if len(req.OrderIDs) == 0 {
			writeError(w, errAPIInvalidRequest)
			return
		}
		if req.DestinationAddress != "" && !isValidTronAddress(req.DestinationAddress) {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		firstSun, repeatSun, firstEnergy, repeatEnergy, energyFee, err := estimateConsolidationFeesSun(
			ctx, deps, req.MasterWalletID, req.DestinationAddress, req.OrderIDs[0],
		)
		switch {
		case errors.Is(err, order.ErrOrderNotFound):
			writeError(w, errAPIOrderNotFound)
			return
		case errors.Is(err, errOrderWalletMismatch):
			writeError(w, errAPIOrderWalletMismatch)
			return
		case errors.Is(err, errRowNotFound):
			writeError(w, errAPIInvalidRequest)
			return
		case errors.Is(err, errEnergyEstimateReverted):
			writeError(w, errAPIEnergyEstimateReverted)
			return
		case errors.Is(err, errTronGridEstimateFailed):
			writeError(w, newAPIError(http.StatusBadGateway, "TRONGRID_ESTIMATE_FAILED", err.Error()))
			return
		case err != nil:
			writeError(w, errAPIInternal)
			return
		}

		writeJSON(w, http.StatusOK, feeEstimateResponse{
			FirstEnergy:  firstEnergy,
			RepeatEnergy: repeatEnergy,
			EnergyFee:    energyFee,
			FirstTRXSun:  firstSun,
			RepeatTRXSun: repeatSun,
			FirstTRX:     formatMicroAmount(firstSun),
			RepeatTRX:    formatMicroAmount(repeatSun),
		})
	}
}

var (
	errOrderWalletMismatch    = errors.New("consolidation: order does not belong to the specified master wallet")
	errEnergyEstimateReverted = errors.New("consolidation: energy estimate simulation reverted on chain")
	errTronGridEstimateFailed = errors.New("consolidation: trongrid estimate call failed")
)

// estimateConsolidationFeesSun computes the first/repeat TRX-sun fee
// estimate for consolidating orders to destinationAddress, via TronGrid's
// triggerconstantcontract simulation (技術架構設計第10節/ADR-0017). Shared by
// feeEstimateHandler (a standalone preview the operator can query before
// submitting) and createFlowHandler (準備歸集's authoritative, automatic
// computation — see ADR-0017 決策 "拿掉手動輸入的每筆金額欄位，改由伺服器自動算好
// 帶去簽名頁", 2026-09-11 使用者拍板). Returns plain sentinel errors so both
// callers can map them to their own response style (JSON vs plain
// http.Error) without duplicating the TronGrid call sequence.
func estimateConsolidationFeesSun(ctx context.Context, deps Deps, masterWalletID int64, destinationAddress string, firstOrderID int64) (firstSun, repeatSun, firstEnergy, repeatEnergy, energyFee int64, err error) {
	params, err := deps.TronClient.GetChainParameters(ctx)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("%w: %v", errTronGridEstimateFailed, err) //nolint:errorlint // wrapping sentinel + dynamic error for errors.Is
	}
	energyFee = params["getEnergyFee"]
	if energyFee <= 0 {
		return 0, 0, 0, 0, 0, errors.New("consolidation: getEnergyFee chain parameter is non-positive")
	}

	ord, err := order.GetByID(ctx, deps.Pool, firstOrderID)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	if ord.MasterWalletID != masterWalletID {
		return 0, 0, 0, 0, 0, errOrderWalletMismatch
	}
	owner := ord.Address

	// repeatEnergy: simulate a transfer FROM owner TO owner itself — owner
	// already holds USDT, so this reads the "not a first-time recipient"
	// energy cost.
	repeatParam, err := abiEncodeTransfer(owner, 1)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	repeatResult, err := deps.TronClient.TriggerConstantContract(ctx, tronclient.TriggerSmartContractParams{
		OwnerAddress:     owner,
		ContractAddress:  deps.USDTContractAddress,
		FunctionSelector: "transfer(address,uint256)",
		Parameter:        repeatParam,
		CallValue:        0,
	})
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("%w: %v", errTronGridEstimateFailed, err) //nolint:errorlint // same as above
	}
	if repeatResult.Reverted {
		return 0, 0, 0, 0, 0, errEnergyEstimateReverted
	}

	// firstEnergy: simulate a transfer FROM owner TO whichever destination is
	// going to actually receive the consolidated funds. If the caller hasn't
	// picked one yet (destinationAddress == ""), fall back to a fixed probe
	// address derived from this master wallet's own xpub — guaranteed to
	// have never held this USDT contract, so it reads a conservative
	// first-time-recipient figure regardless of what real destination gets
	// chosen later.
	firstTarget := destinationAddress
	if firstTarget == "" {
		wallet, werr := getMasterWallet(ctx, deps.Pool, masterWalletID)
		if werr != nil {
			return 0, 0, 0, 0, 0, werr
		}
		firstTarget, err = hdwallet.DeriveAddress(wallet.Xpub, consolidationProbeIndex)
		if err != nil {
			return 0, 0, 0, 0, 0, err
		}
	}

	firstParam, err := abiEncodeTransfer(firstTarget, 1)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	firstResult, err := deps.TronClient.TriggerConstantContract(ctx, tronclient.TriggerSmartContractParams{
		OwnerAddress:     owner,
		ContractAddress:  deps.USDTContractAddress,
		FunctionSelector: "transfer(address,uint256)",
		Parameter:        firstParam,
		CallValue:        0,
	})
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("%w: %v", errTronGridEstimateFailed, err) //nolint:errorlint // same as above
	}
	if firstResult.Reverted {
		return 0, 0, 0, 0, 0, errEnergyEstimateReverted
	}

	firstSun = energyToTRXSun(firstResult.EnergyUsed, energyFee)
	repeatSun = energyToTRXSun(repeatResult.EnergyUsed, energyFee)
	return firstSun, repeatSun, firstResult.EnergyUsed, repeatResult.EnergyUsed, energyFee, nil
}

// trxBalanceRequest is POST /admin/consolidation/trx-balance's request body.
type trxBalanceRequest struct {
	Address string `json:"address"`
}

type trxBalanceResponse struct {
	BalanceSun int64  `json:"balance_sun"`
	BalanceTRX string `json:"balance_trx"`
}

// trxBalanceHandler implements POST /admin/consolidation/trx-balance: a
// read-only native-TRX balance lookup (deps.TronClient.AccountTRXBalance),
// used by the sign page to show an address's current TRX before/after a
// fee top-up without the operator needing to leave the page.
func trxBalanceHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req trxBalanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, errAPIInvalidRequest)
			return
		}
		if !isValidTronAddress(req.Address) {
			writeError(w, errAPIInvalidRequest)
			return
		}
		ctx := r.Context()

		balance, err := deps.TronClient.AccountTRXBalance(ctx, req.Address)
		if err != nil {
			writeError(w, newAPIError(http.StatusBadGateway, "TRONGRID_BALANCE_FAILED", err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, trxBalanceResponse{
			BalanceSun: balance,
			BalanceTRX: formatMicroAmount(balance),
		})
	}
}
