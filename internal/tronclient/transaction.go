package tronclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

// defaultFeeLimit is the max TRX (in sun) TriggerSmartContract is willing
// to let a single call spend on energy — a safety cap, not the actual fee
// (unused headroom under this cap is never charged). 100 TRX comfortably
// covers a USDT transfer(), which 驗證結論-06 measured at roughly 13,000+
// energy in practice.
const defaultFeeLimit int64 = 100_000_000

// ErrBroadcastNetworkFailure means BroadcastTransaction could not complete
// the HTTP round-trip to TronGrid — this does NOT mean the transaction was
// not broadcast (驗證結論-06 觀察4 recorded a real case of exactly this
// happening, followed by the transaction having actually succeeded).
// Callers must reconcile via TransactionInfo before assuming failure, per
// CLAUDE.md 安全鐵律7.
var ErrBroadcastNetworkFailure = errors.New("tronclient: broadcast network failure — unknown whether TronGrid received the transaction")

// PreparedTransaction is the unsigned transaction TronGrid returns from
// TriggerSmartContract/CreateTransaction. Transaction is kept as opaque
// JSON (raw_data's contract[].parameter shape is polymorphic per contract
// type) — callers only need TxID for verification and the Transaction blob
// to hand back to the browser for signing; nothing else in this package
// needs to understand its internals.
type PreparedTransaction struct {
	TxID        string
	Transaction json.RawMessage
}

// TriggerSmartContractParams builds a request to
// POST /wallet/triggersmartcontract.
type TriggerSmartContractParams struct {
	OwnerAddress     string
	ContractAddress  string
	FunctionSelector string
	Parameter        string // ABI-encoded hex, no 0x prefix
	CallValue        int64
}

// TriggerSmartContract calls TronGrid to build (not sign or broadcast) a
// smart-contract-call transaction — safe to retry (doPostRequest), it has
// no side effects on chain.
func (c *Client) TriggerSmartContract(ctx context.Context, p TriggerSmartContractParams) (PreparedTransaction, error) {
	reqBody, err := json.Marshal(struct {
		OwnerAddress     string `json:"owner_address"`
		ContractAddress  string `json:"contract_address"`
		FunctionSelector string `json:"function_selector"`
		Parameter        string `json:"parameter"`
		FeeLimit         int64  `json:"fee_limit"`
		CallValue        int64  `json:"call_value"`
		Visible          bool   `json:"visible"`
	}{p.OwnerAddress, p.ContractAddress, p.FunctionSelector, p.Parameter, defaultFeeLimit, p.CallValue, true})
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: TriggerSmartContract: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/triggersmartcontract", reqBody)
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: TriggerSmartContract: %w", err)
	}

	var raw struct {
		Transaction json.RawMessage `json:"transaction"`
		Result      struct {
			Result  bool   `json:"result"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: TriggerSmartContract: parse response: %w", err)
	}
	if !raw.Result.Result {
		return PreparedTransaction{}, fmt.Errorf("tronclient: TriggerSmartContract: %s: %s", raw.Result.Code, decodeMaybeHex(raw.Result.Message))
	}

	txID, err := extractTxID(raw.Transaction)
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: TriggerSmartContract: %w", err)
	}
	return PreparedTransaction{TxID: txID, Transaction: raw.Transaction}, nil
}

// CreateTransactionParams builds a request to POST /wallet/createtransaction
// (a native TRX TransferContract).
type CreateTransactionParams struct {
	OwnerAddress string
	ToAddress    string
	Amount       int64 // sun
}

// CreateTransaction calls TronGrid to build (not sign or broadcast) a
// native TRX transfer transaction — safe to retry, no side effects.
func (c *Client) CreateTransaction(ctx context.Context, p CreateTransactionParams) (PreparedTransaction, error) {
	reqBody, err := json.Marshal(struct {
		OwnerAddress string `json:"owner_address"`
		ToAddress    string `json:"to_address"`
		Amount       int64  `json:"amount"`
		Visible      bool   `json:"visible"`
	}{p.OwnerAddress, p.ToAddress, p.Amount, true})
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: CreateTransaction: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/createtransaction", reqBody)
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: CreateTransaction: %w", err)
	}

	// createtransaction's failure shape is {"Error": "<plain text>"} —
	// confirmed against a real Shasta response during this module's
	// development to be genuinely different from triggersmartcontract's/
	// broadcasttransaction's {"code","message"} shape (and NOT hex-encoded).
	var errResp struct {
		Error string `json:"Error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return PreparedTransaction{}, fmt.Errorf("tronclient: CreateTransaction: %s", errResp.Error)
	}

	// On success the transaction object is the top-level response body
	// itself (unlike triggersmartcontract, which wraps it under "transaction").
	txID, err := extractTxID(body)
	if err != nil {
		return PreparedTransaction{}, fmt.Errorf("tronclient: CreateTransaction: %w", err)
	}
	return PreparedTransaction{TxID: txID, Transaction: json.RawMessage(body)}, nil
}

// BroadcastResult is TronGrid's response to broadcasttransaction.
type BroadcastResult struct {
	Success bool
	TxID    string
	Code    string
	Message string
}

// BroadcastTransaction submits a fully signed transaction. It makes exactly
// ONE HTTP attempt and never retries — see ErrBroadcastNetworkFailure and
// CLAUDE.md 安全鐵律7: a network-layer failure here does not mean the
// transaction wasn't broadcast, so blindly retrying (or the generic
// doPostRequest's retry loop) risks a duplicate broadcast. Callers must
// treat ErrBroadcastNetworkFailure as "unknown, reconcile via
// TransactionInfo later" — never as "failed, safe to retry immediately".
func (c *Client) BroadcastTransaction(ctx context.Context, signedTransaction json.RawMessage) (BroadcastResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/wallet/broadcasttransaction", bytes.NewReader(signedTransaction))
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("tronclient: BroadcastTransaction: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("%w: %v", ErrBroadcastNetworkFailure, err) //nolint:errorlint // deliberately wrapping a dynamic error alongside a sentinel for errors.Is
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return BroadcastResult{}, fmt.Errorf("%w: read response: %v", ErrBroadcastNetworkFailure, err) //nolint:errorlint // same as above
	}

	var raw struct {
		Result  bool   `json:"result"`
		TxID    string `json:"txid"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		// TronGrid DID respond — this is a parse failure, not a network
		// failure, so it must not be reported as ErrBroadcastNetworkFailure
		// (that would incorrectly tell the caller to treat this as "maybe
		// broadcast, reconcile later" when we know for certain a malformed
		// response came back and no sane broadcast happened).
		return BroadcastResult{}, fmt.Errorf("tronclient: BroadcastTransaction: parse response: %w", err)
	}

	return BroadcastResult{
		Success: raw.Result,
		TxID:    raw.TxID,
		Code:    raw.Code,
		Message: decodeMaybeHex(raw.Message),
	}, nil
}

// TransactionReceipt mirrors gettransactioninfobyid's "receipt" object.
// Result is only populated for TriggerSmartContract transactions
// ("SUCCESS"/"REVERT"/...) — native TransferContract transactions have no
// receipt.result field at all (驗證結論-06 附加測試), which surfaces here as
// an empty string; callers must branch on contract type, not assume Result
// is always present.
type TransactionReceipt struct {
	Result           string
	NetFee           int64
	EnergyFee        int64
	EnergyUsageTotal int64
}

// TransactionLog mirrors one entry of gettransactioninfobyid's "log" array
// (contract event logs, e.g. the TRC20 Transfer event).
type TransactionLog struct {
	Address string
	Topics  []string
	Data    string
}

// TransactionInfo is the result of gettransactioninfobyid. Found is false
// when TronGrid doesn't know about the transaction yet — confirmed against
// a real Shasta response to be a bare `{}` object, not a 404 or an
// explicit "not found" field.
type TransactionInfo struct {
	Found       bool
	BlockNumber int64
	Receipt     TransactionReceipt
	Log         []TransactionLog
}

// TransactionInfo queries the on-chain execution result of txID — safe to
// retry, read-only. This is the source of truth for whether a broadcast
// actually happened and succeeded (CLAUDE.md 安全鐵律7): a network-layer
// failure from BroadcastTransaction must be resolved by calling this, not
// by re-broadcasting.
func (c *Client) TransactionInfo(ctx context.Context, txID string) (TransactionInfo, error) {
	reqBody, err := json.Marshal(struct {
		Value string `json:"value"`
	}{txID})
	if err != nil {
		return TransactionInfo{}, fmt.Errorf("tronclient: TransactionInfo: build request body: %w", err)
	}

	body, err := c.doPostRequest(ctx, "/wallet/gettransactioninfobyid", reqBody)
	if err != nil {
		return TransactionInfo{}, fmt.Errorf("tronclient: TransactionInfo: %w", err)
	}

	var raw struct {
		ID          string `json:"id"`
		BlockNumber int64  `json:"blockNumber"`
		Receipt     struct {
			Result           string `json:"result"`
			NetFee           int64  `json:"net_fee"`
			EnergyFee        int64  `json:"energy_fee"`
			EnergyUsageTotal int64  `json:"energy_usage_total"`
		} `json:"receipt"`
		Log []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"log"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return TransactionInfo{}, fmt.Errorf("tronclient: TransactionInfo: parse response: %w", err)
	}

	if raw.ID == "" {
		return TransactionInfo{Found: false}, nil
	}

	info := TransactionInfo{
		Found:       true,
		BlockNumber: raw.BlockNumber,
		Receipt: TransactionReceipt{
			Result:           raw.Receipt.Result,
			NetFee:           raw.Receipt.NetFee,
			EnergyFee:        raw.Receipt.EnergyFee,
			EnergyUsageTotal: raw.Receipt.EnergyUsageTotal,
		},
	}
	for _, l := range raw.Log {
		info.Log = append(info.Log, TransactionLog{Address: l.Address, Topics: l.Topics, Data: l.Data})
	}
	return info, nil
}

func extractTxID(transaction json.RawMessage) (string, error) {
	var t struct {
		TxID string `json:"txID"`
	}
	if err := json.Unmarshal(transaction, &t); err != nil {
		return "", fmt.Errorf("parse transaction txID: %w", err)
	}
	if t.TxID == "" {
		return "", fmt.Errorf("transaction response missing txID")
	}
	return t.TxID, nil
}

// decodeMaybeHex best-effort-decodes hex-encoded error messages (TronGrid
// sometimes hex-encodes these, e.g. contract revert reasons) while leaving
// plain-text messages untouched. This module's development found real
// examples of BOTH forms coming back from the same endpoint family (a
// SIGERROR validation message came back as plain text, not hex, despite
// TronGrid's own docs showing a hex example for a different error code) —
// this is only used for a human-readable error_detail field, never for
// broadcast-outcome decisions, so a wrong guess here has no money-safety
// consequence.
func decodeMaybeHex(s string) string {
	if s == "" {
		return s
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return s
	}
	if !utf8.Valid(decoded) {
		return s
	}
	for _, r := range string(decoded) {
		if r < 0x09 || (r > 0x0d && r < 0x20) {
			return s
		}
	}
	return string(decoded)
}
