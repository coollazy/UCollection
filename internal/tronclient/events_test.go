package tronclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// realEventsFixture is a real response captured from
// GET https://api.trongrid.io/v1/contracts/TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t/events?event_name=Transfer&only_confirmed=true&limit=2
// during design of this package. The hex->Base58 address conversion below
// was independently confirmed by querying GET /v1/accounts/{address} for
// the converted address and observing it echo the same address back in its
// own owner_permission — see this package's plan notes.
const realEventsFixture = `{"data":[{"block_number":85919983,"block_timestamp":1788431625000,"caller_contract_address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","contract_address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","event_index":0,"event_name":"Transfer","result":{"0":"0x01311788a9736dcc38cf6d53342c076c38e95ef2","1":"0xb2ff692d318825a41b5668c4cefa263e8cdcc82e","2":"1399000000","from":"0x01311788a9736dcc38cf6d53342c076c38e95ef2","to":"0xb2ff692d318825a41b5668c4cefa263e8cdcc82e","value":"1399000000"},"result_type":{"from":"address","to":"address","value":"uint256"},"event":"Transfer(address indexed from, address indexed to, uint256 value)","transaction_id":"1d8e417344c69d0355fcd6f71aae6575aec4911ffb706a2d3169e43728d6a2e2"},{"block_number":85919983,"block_timestamp":1788431625000,"caller_contract_address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","contract_address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t","event_index":0,"event_name":"Transfer","result":{"0":"0xbe090b720b5228f04049fbe2ef815da7b89f6277","1":"0xac9e825d1245a117fbd24989a9a4a65520ea93ea","2":"2430000000","from":"0xbe090b720b5228f04049fbe2ef815da7b89f6277","to":"0xac9e825d1245a117fbd24989a9a4a65520ea93ea","value":"2430000000"},"result_type":{"from":"address","to":"address","value":"uint256"},"event":"Transfer(address indexed from, address indexed to, uint256 value)","transaction_id":"0ec21a086bbaf72740699834ab13f4db482d64264c99095ae869e237e4ca4ce2"}],"success":true,"meta":{"at":1788431685775,"fingerprint":"7dveEb1DxX4L2Y6qWLhPFmx9Nyf5ZJTzv2oK9iY8TB3PSizWBNpw4RjYmDySAcGzXgZgvinB7wRBDJvm63FzHudXX89bUc1kefL2jRoN362SCTZDVatq15FW8HXTs1r7FJUSNXxtN4W4SJL4hrEhFijakJb46B9VUkoeDnXhzP3SmyZ3H3FG3gyMaUoyJSYkC2DBrfkXu3BsEC7LxVV9ZjFZbkjDJaMPPHzZMyGGRj8","links":{"next":"https://api.trongrid.io/v1/contracts/TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t/events?event_name=Transfer&limit=2&only_confirmed=true&fingerprint=7dveEb1DxX4L2Y6qWLhPFmx9Nyf5ZJTzv2oK9iY8TB3PSizWBNpw4RjYmDySAcGzXgZgvinB7wRBDJvm63FzHudXX89bUc1kefL2jRoN362SCTZDVatq15FW8HXTs1r7FJUSNXxtN4W4SJL4hrEhFijakJb46B9VUkoeDnXhzP3SmyZ3H3FG3gyMaUoyJSYkC2DBrfkXu3BsEC7LxVV9ZjFZbkjDJaMPPHzZMyGGRj8"},"page_size":2}}`

func TestContractEvents_ParsesRealFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("event_name"); got != "Transfer" {
			t.Errorf("event_name query param = %q, want Transfer", got)
		}
		if got := r.URL.Query().Get("only_confirmed"); got != "true" {
			t.Errorf("only_confirmed query param = %q, want true", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(realEventsFixture))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	page, err := c.ContractEvents(context.Background(), "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", EventsQuery{
		EventName:     "Transfer",
		OnlyConfirmed: true,
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("ContractEvents() error = %v", err)
	}

	if len(page.Events) != 2 {
		t.Fatalf("len(page.Events) = %d, want 2", len(page.Events))
	}

	first := page.Events[0]
	wantFrom := "TA5WVDb8QG5SoPNk1HCnombmkS9nWqH9LR" // verified independently against a live TronGrid account lookup
	if first.From != wantFrom {
		t.Errorf("Events[0].From = %q, want %q", first.From, wantFrom)
	}
	wantTo := "TSHfFTij9Ch7MEcs8c67b9iPTCYmxrwsVV"
	if first.To != wantTo {
		t.Errorf("Events[0].To = %q, want %q", first.To, wantTo)
	}
	if first.Value != 1399000000 {
		t.Errorf("Events[0].Value = %d, want 1399000000", first.Value)
	}
	if first.EventIndex != 0 {
		t.Errorf("Events[0].EventIndex = %d, want 0", first.EventIndex)
	}
	if first.BlockNumber != 85919983 {
		t.Errorf("Events[0].BlockNumber = %d, want 85919983", first.BlockNumber)
	}
	if first.TransactionID != "1d8e417344c69d0355fcd6f71aae6575aec4911ffb706a2d3169e43728d6a2e2" {
		t.Errorf("Events[0].TransactionID = %q, unexpected", first.TransactionID)
	}

	wantFingerprint := "7dveEb1DxX4L2Y6qWLhPFmx9Nyf5ZJTzv2oK9iY8TB3PSizWBNpw4RjYmDySAcGzXgZgvinB7wRBDJvm63FzHudXX89bUc1kefL2jRoN362SCTZDVatq15FW8HXTs1r7FJUSNXxtN4W4SJL4hrEhFijakJb46B9VUkoeDnXhzP3SmyZ3H3FG3gyMaUoyJSYkC2DBrfkXu3BsEC7LxVV9ZjFZbkjDJaMPPHzZMyGGRj8"
	if page.NextFingerprint != wantFingerprint {
		t.Errorf("NextFingerprint = %q, want %q", page.NextFingerprint, wantFingerprint)
	}
}

func TestContractEvents_NoNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"success":true,"meta":{"at":1788430422267,"page_size":200}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	page, err := c.ContractEvents(context.Background(), "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", EventsQuery{EventName: "Transfer"})
	if err != nil {
		t.Fatalf("ContractEvents() error = %v", err)
	}
	if page.NextFingerprint != "" {
		t.Errorf("NextFingerprint = %q, want empty (no next page)", page.NextFingerprint)
	}
	if len(page.Events) != 0 {
		t.Errorf("len(page.Events) = %d, want 0", len(page.Events))
	}
}

func TestHexToTronAddress(t *testing.T) {
	const want = "TXj77r7qkzvswyKAy48ZY3w4EBoosJn46P"

	tests := []struct {
		name string
		hex  string
	}{
		{"with 0x prefix", "0xeea8086e76430e09a64a58b39a8b03899db194c6"},
		{"without prefix", "eea8086e76430e09a64a58b39a8b03899db194c6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hexToTronAddress(tt.hex)
			if err != nil {
				t.Fatalf("hexToTronAddress(%q) error = %v", tt.hex, err)
			}
			if got != want {
				t.Errorf("hexToTronAddress(%q) = %q, want %q", tt.hex, got, want)
			}
		})
	}
}

func TestHexToTronAddress_InvalidHex(t *testing.T) {
	if _, err := hexToTronAddress("not-hex"); err == nil {
		t.Fatal("hexToTronAddress() error = nil, want error for malformed hex")
	}
}
