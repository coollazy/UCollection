package tronclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// rawAccountResponse mirrors GET /v1/accounts/{address}. trc20 is an array
// of single-key objects (contract address -> decimal balance string), not a
// map keyed by contract the way you might expect — confirmed against a real
// TronGrid response during this module's development (see docs/進度.md).
type rawAccountResponse struct {
	Data []struct {
		TRC20 []map[string]string `json:"trc20"`
	} `json:"data"`
}

// TRC20Balance returns address's balance of contractAddress (smallest
// unit, matching CLAUDE.md 安全鐵律6's int64-only rule). An address that has
// never interacted with the token (or never appeared on chain at all) has
// no entry for it, which is a balance of 0, not an error.
func (c *Client) TRC20Balance(ctx context.Context, address, contractAddress string) (int64, error) {
	body, err := c.doRequest(ctx, "/v1/accounts/"+address)
	if err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: %w", err)
	}

	var raw rawAccountResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, fmt.Errorf("tronclient: TRC20Balance: parse response: %w", err)
	}
	if len(raw.Data) == 0 {
		return 0, nil
	}

	for _, entry := range raw.Data[0].TRC20 {
		balanceStr, ok := entry[contractAddress]
		if !ok {
			continue
		}
		balance, err := strconv.ParseInt(balanceStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("tronclient: TRC20Balance: parse balance %q: %w", balanceStr, err)
		}
		return balance, nil
	}
	return 0, nil
}
