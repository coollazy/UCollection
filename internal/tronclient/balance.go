package tronclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/btcutil/base58"
)

// balanceOfSelector is the TRC20 balanceOf(address) function this package
// queries via triggerconstantcontract.
const balanceOfSelector = "balanceOf(address)"

// TRC20Balance returns address's balance of contractAddress (smallest
// unit, matching CLAUDE.md 安全鐵律6's int64-only rule), via a read-only
// triggerconstantcontract call to the token contract's balanceOf. This
// queries the contract's actual storage directly rather than going through
// GET /v1/accounts/{address} — that indexed endpoint was found to return an
// empty account (and therefore a false balance of 0) for addresses that
// have received USDT but never had any other on-chain activity, e.g. never
// received TRX (docs/進度.md 驗證測試2026-09-09 條目8：查得到的實際餘額用真實
// Shasta測試網地址核對過). triggerconstantcontract is unaffected by that gap
// since it executes against current chain state, not an indexer snapshot.
func (c *Client) TRC20Balance(ctx context.Context, address, contractAddress string) (int64, error) {
	param, err := encodeAddressParam(address)
	if err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: %w", err)
	}

	reqBody, err := json.Marshal(struct {
		OwnerAddress     string `json:"owner_address"`
		ContractAddress  string `json:"contract_address"`
		FunctionSelector string `json:"function_selector"`
		Parameter        string `json:"parameter"`
		Visible          bool   `json:"visible"`
	}{address, contractAddress, balanceOfSelector, param, true})
	if err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/triggerconstantcontract", reqBody)
	if err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: %w", err)
	}

	var raw struct {
		ConstantResult []string `json:"constant_result"`
		Result         struct {
			Result  bool   `json:"result"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: parse response: %w", err)
	}
	if !raw.Result.Result {
		return 0, fmt.Errorf("tronclient: TRC20Balance: %s: %s", raw.Result.Code, decodeMaybeHex(raw.Result.Message))
	}
	if len(raw.ConstantResult) == 0 || raw.ConstantResult[0] == "" {
		return 0, nil
	}

	balanceBytes, err := hex.DecodeString(raw.ConstantResult[0])
	if err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: decode constant_result %q: %w", raw.ConstantResult[0], err)
	}
	balance := new(big.Int).SetBytes(balanceBytes)
	if !balance.IsInt64() {
		return 0, fmt.Errorf("tronclient: TRC20Balance: balance %s overflows int64", balance.String())
	}
	return balance.Int64(), nil
}

// encodeAddressParam ABI-encodes a single Tron address argument
// (left-padded to 32 bytes, no function selector prefix) — same encoding
// convention as internal/consolidation's abiEncodeTransfer destination
// parameter.
func encodeAddressParam(addr string) (string, error) {
	payload, version, err := base58.CheckDecode(addr)
	if err != nil {
		return "", fmt.Errorf("decode address %q: %w", addr, err)
	}
	if version != tronAddressVersion || len(payload) != 20 {
		return "", fmt.Errorf("%q is not a valid Tron address", addr)
	}
	var param [32]byte
	copy(param[12:], payload)
	return hex.EncodeToString(param[:]), nil
}
