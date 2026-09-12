package consolidation

import (
	"strings"
	"testing"

	"github.com/coollazy/UCollection/internal/order"
)

func TestFormatMicroAmount(t *testing.T) {
	cases := []struct {
		amount int64
		want   string
	}{
		{0, "0"},
		{30_000000, "30"},
		{30_500000, "30.5"},
		{30_050000, "30.05"},
		{30_000001, "30.000001"},
		{1, "0.000001"},
	}
	for _, c := range cases {
		if got := formatMicroAmount(c.amount); got != c.want {
			t.Errorf("formatMicroAmount(%d) = %q, want %q", c.amount, got, c.want)
		}
	}
}

func TestStatusPill(t *testing.T) {
	cases := []struct {
		status    string
		wantClass string
	}{
		{"not_consolidated", "pill--not-consolidated"},
		{"consolidated", "pill--completed"},
		{"pending", "pill--pending"},
		{"broadcasting", "pill--confirming"},
		{"success", "pill--completed"},
		{"failed", "pill--stalled"},
	}
	for _, c := range cases {
		got := string(statusPill(c.status))
		if !strings.Contains(got, `class="pill `+c.wantClass+`"`) {
			t.Errorf("statusPill(%q) = %q, want class %q", c.status, got, c.wantClass)
		}
	}
}

// TestStatusPill_NamedStringType matches internal/admin's identical guard
// — see that package's test for the real bug this caught during P2.
func TestStatusPill_NamedStringType(t *testing.T) {
	got := string(statusPill(order.StatusPending))
	if !strings.Contains(got, `class="pill pill--pending"`) {
		t.Errorf("statusPill(order.StatusPending) = %q, want pill--pending class", got)
	}
}
