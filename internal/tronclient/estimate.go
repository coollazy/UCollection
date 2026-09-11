package tronclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ConstantContractResult is the result of a read-only
// triggerconstantcontract call — an energy-cost simulation, not an
// on-chain state change (no fee is charged, nothing is broadcast).
type ConstantContractResult struct {
	EnergyUsed     int64
	ConstantResult []string
	Reverted       bool
	Message        string
}

// TriggerConstantContract calls TronGrid's triggerconstantcontract to
// simulate a smart-contract call (e.g. a USDT transfer()) and read back the
// energy it would consume, without broadcasting anything. Used by
// internal/consolidation to estimate a transfer's TRX fee before building
// the real transaction. Safe to retry (doPostRequest): it has no side
// effects on chain.
//
// Reverted is true when result.result is false OR when the (possibly
// hex-encoded) result.message contains "REVERT" — a real Shasta response
// was observed with result.result=true but message containing "REVERT
// opcode executed" (owner had no token balance), so result.result alone is
// not a reliable revert signal. Callers must treat Reverted=true as "the
// EnergyUsed figure is not trustworthy for fee estimation", not just as a
// generic failure flag.
func (c *Client) TriggerConstantContract(ctx context.Context, p TriggerSmartContractParams) (ConstantContractResult, error) {
	reqBody, err := json.Marshal(struct {
		OwnerAddress     string `json:"owner_address"`
		ContractAddress  string `json:"contract_address"`
		FunctionSelector string `json:"function_selector"`
		Parameter        string `json:"parameter"`
		CallValue        int64  `json:"call_value"`
		Visible          bool   `json:"visible"`
	}{p.OwnerAddress, p.ContractAddress, p.FunctionSelector, p.Parameter, p.CallValue, true})
	if err != nil {
		return ConstantContractResult{}, fmt.Errorf("tronclient: TriggerConstantContract: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/triggerconstantcontract", reqBody)
	if err != nil {
		return ConstantContractResult{}, fmt.Errorf("tronclient: TriggerConstantContract: %w", err)
	}

	var raw struct {
		EnergyUsed     int64    `json:"energy_used"`
		ConstantResult []string `json:"constant_result"`
		Result         struct {
			Result  bool   `json:"result"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ConstantContractResult{}, fmt.Errorf("tronclient: TriggerConstantContract: parse response: %w", err)
	}

	message := decodeMaybeHex(raw.Result.Message)
	reverted := !raw.Result.Result || strings.Contains(message, "REVERT")

	return ConstantContractResult{
		EnergyUsed:     raw.EnergyUsed,
		ConstantResult: raw.ConstantResult,
		Reverted:       reverted,
		Message:        message,
	}, nil
}

// GetChainParameters returns TronGrid's current network-wide chain
// parameters (e.g. getEnergyFee, getTotalEnergyLimit) as a key→value map.
// Used by internal/consolidation to convert a TriggerConstantContract
// energy estimate into a TRX fee estimate. Safe to retry, read-only.
//
// Some chainParameter entries omit the "value" field entirely when their
// value is 0 (confirmed against a real response) — those simply decode to
// Go's int64 zero value, which is the correct value, not an error.
func (c *Client) GetChainParameters(ctx context.Context) (map[string]int64, error) {
	body, err := c.doPostRequest(ctx, "/wallet/getchainparameters", []byte(`{}`))
	if err != nil {
		return nil, fmt.Errorf("tronclient: GetChainParameters: %w", err)
	}

	var raw struct {
		ChainParameter []struct {
			Key   string `json:"key"`
			Value int64  `json:"value"`
		} `json:"chainParameter"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tronclient: GetChainParameters: parse response: %w", err)
	}

	params := make(map[string]int64, len(raw.ChainParameter))
	for _, p := range raw.ChainParameter {
		params[p.Key] = p.Value
	}
	return params, nil
}

// AccountTRXBalance returns address's native TRX balance (sun), via
// getaccount. Used by internal/consolidation to check whether a collection
// wallet holds enough TRX to cover the estimated fee before building a
// real transfer. Safe to retry, read-only.
//
// An unactivated/never-used account's response is a bare `{}` (confirmed
// against a real response, mirroring TransactionInfo's Found pattern in
// transaction.go) — that decodes to a zero Balance field, so it correctly
// returns 0, nil rather than an error.
func (c *Client) AccountTRXBalance(ctx context.Context, address string) (int64, error) {
	reqBody, err := json.Marshal(struct {
		Address string `json:"address"`
		Visible bool   `json:"visible"`
	}{address, true})
	if err != nil {
		return 0, fmt.Errorf("tronclient: AccountTRXBalance: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/getaccount", reqBody)
	if err != nil {
		return 0, fmt.Errorf("tronclient: AccountTRXBalance: %w", err)
	}

	var raw struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, fmt.Errorf("tronclient: AccountTRXBalance: parse response: %w", err)
	}
	return raw.Balance, nil
}
