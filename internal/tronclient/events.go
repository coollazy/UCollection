package tronclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/btcutil/base58"
)

// tronAddressVersion is the single version byte Tron prepends before
// Base58Check-encoding an address (0x41), same convention used in
// internal/hdwallet.
const tronAddressVersion = 0x41

// Event is a single Transfer event returned by TronGrid's contract events
// endpoint, with addresses already converted from TronGrid's raw hex
// format to Tron's Base58Check format (matching how addresses are stored
// in orders.address — see docs/開發流程框架-03-技術架構設計.md 第2節).
type Event struct {
	TransactionID  string
	EventIndex     int
	BlockNumber    int64
	BlockTimestamp int64
	From           string
	To             string
	// Value is the transfer amount in the token's smallest unit (USDT has
	// 6 decimals), never a float — see CLAUDE.md 安全鐵律6.
	Value int64
}

// EventsPage is one page of a contract events query.
type EventsPage struct {
	Events []Event
	// NextFingerprint is the pagination cursor for the next page, or ""
	// if this was the last page.
	NextFingerprint string
}

// EventsQuery configures a ContractEvents call.
type EventsQuery struct {
	EventName         string
	OnlyConfirmed     bool
	MinBlockTimestamp int64
	// Fingerprint continues a previous page (EventsPage.NextFingerprint).
	// Zero value means "start from the first page".
	Fingerprint string
	// Limit is the page size. Zero means "use TronGrid's default", but
	// scanner/reverify callers should pass 200 (技術架構設計第4節：單輪結果
	// 達200筆上限時用fingerprint翻頁).
	Limit int
}

type rawEventsResponse struct {
	Data []rawEvent `json:"data"`
	Meta struct {
		Fingerprint string `json:"fingerprint"`
	} `json:"meta"`
}

type rawEvent struct {
	TransactionID  string `json:"transaction_id"`
	EventIndex     int    `json:"event_index"`
	BlockNumber    int64  `json:"block_number"`
	BlockTimestamp int64  `json:"block_timestamp"`
	Result         struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Value string `json:"value"`
	} `json:"result"`
}

// ContractEvents queries TronGrid's GET /v1/contracts/{contractAddress}/events
// for a single page of results. Callers needing every event in range must
// keep calling with EventsQuery.Fingerprint set to the previous
// EventsPage.NextFingerprint until it comes back empty.
func (c *Client) ContractEvents(ctx context.Context, contractAddress string, q EventsQuery) (EventsPage, error) {
	params := url.Values{}
	params.Set("event_name", q.EventName)
	params.Set("only_confirmed", strconv.FormatBool(q.OnlyConfirmed))
	if q.MinBlockTimestamp > 0 {
		params.Set("min_block_timestamp", strconv.FormatInt(q.MinBlockTimestamp, 10))
	}
	if q.Fingerprint != "" {
		params.Set("fingerprint", q.Fingerprint)
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}

	path := fmt.Sprintf("/v1/contracts/%s/events?%s", contractAddress, params.Encode())

	body, err := c.doRequest(ctx, path)
	if err != nil {
		return EventsPage{}, err
	}

	var raw rawEventsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return EventsPage{}, fmt.Errorf("tronclient: decode events response: %w", err)
	}

	events := make([]Event, 0, len(raw.Data))
	for _, re := range raw.Data {
		from, err := hexToTronAddress(re.Result.From)
		if err != nil {
			return EventsPage{}, fmt.Errorf("tronclient: event %s from address: %w", re.TransactionID, err)
		}
		to, err := hexToTronAddress(re.Result.To)
		if err != nil {
			return EventsPage{}, fmt.Errorf("tronclient: event %s to address: %w", re.TransactionID, err)
		}
		value, err := strconv.ParseInt(re.Result.Value, 10, 64)
		if err != nil {
			return EventsPage{}, fmt.Errorf("tronclient: event %s value %q: %w", re.TransactionID, re.Result.Value, err)
		}

		events = append(events, Event{
			TransactionID:  re.TransactionID,
			EventIndex:     re.EventIndex,
			BlockNumber:    re.BlockNumber,
			BlockTimestamp: re.BlockTimestamp,
			From:           from,
			To:             to,
			Value:          value,
		})
	}

	return EventsPage{Events: events, NextFingerprint: raw.Meta.Fingerprint}, nil
}

// hexToTronAddress converts a TronGrid raw hex address (optionally
// "0x"-prefixed, 20 bytes, no version byte) into Tron's Base58Check
// address format (the format stored in orders.address). See this
// package's plan notes: verified against a live TronGrid account lookup
// during design, not assumed from documentation alone.
func hexToTronAddress(h string) (string, error) {
	h = strings.TrimPrefix(h, "0x")
	raw, err := hex.DecodeString(h)
	if err != nil {
		return "", fmt.Errorf("decode hex address %q: %w", h, err)
	}
	return base58.CheckEncode(raw, tronAddressVersion), nil
}
