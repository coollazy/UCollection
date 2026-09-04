package tronclient

import (
	"context"
	"net/http"
	"testing"
)

// Trimmed to the fields this package parses; captured from a real Shasta
// account during this module's development (see docs/進度.md).
const accountWithTRC20Fixture = `{"data":[{"address":"41c8599111f29c1e1e061265b4af93ea1f274ad78a","trc20":[{"TC2jADz2rJEydJMDiNjncmSzzanWySdc51":"1500000"},{"TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs":"1000822224"}]}],"success":true,"meta":{"at":1788505932993,"page_size":1}}`

func TestTRC20Balance_ContractPresent(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, accountWithTRC20Fixture)
	c := NewClient(srv.URL, "")

	got, err := c.TRC20Balance(context.Background(), "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err != nil {
		t.Fatalf("TRC20Balance() error = %v", err)
	}
	if got != 1000822224 {
		t.Errorf("TRC20Balance() = %d, want 1000822224", got)
	}
}

func TestTRC20Balance_ContractAbsent(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, accountWithTRC20Fixture)
	c := NewClient(srv.URL, "")

	got, err := c.TRC20Balance(context.Background(), "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH", "TSomeOtherContractNotHeldByThisAccount")
	if err != nil {
		t.Fatalf("TRC20Balance() error = %v", err)
	}
	if got != 0 {
		t.Errorf("TRC20Balance() = %d, want 0 for a contract this account has never held", got)
	}
}

func TestTRC20Balance_AccountNeverSeenOnChain(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{"data":[],"success":true,"meta":{}}`)
	c := NewClient(srv.URL, "")

	got, err := c.TRC20Balance(context.Background(), "TBrandNewNeverUsedAddress", "TG3XXyExBkPp9nzdajDZsozEu4BkaSJozs")
	if err != nil {
		t.Fatalf("TRC20Balance() error = %v", err)
	}
	if got != 0 {
		t.Errorf("TRC20Balance() = %d, want 0 for an address with no on-chain history", got)
	}
}
