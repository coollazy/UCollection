package admin

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
		{"PENDING", "pill--pending"},
		{"CONFIRMING", "pill--confirming"},
		{"COMPLETED", "pill--completed"},
		{"OVERPAID", "pill--overpaid"},
		{"EXPIRED", "pill--expired"},
		{"CONFIRMATION_STALLED", "pill--stalled"},
		{"not_consolidated", "pill--not-consolidated"},
		{"consolidated", "pill--completed"},
		{"pending", "pill--pending"},
		{"broadcasting", "pill--confirming"},
		{"success", "pill--completed"},
		{"failed", "pill--stalled"},
		{"sending", "pill--confirming"},
		{"delivered", "pill--completed"},
		{"awaiting_config", "pill--not-consolidated"},
	}
	for _, c := range cases {
		got := string(statusPill(c.status))
		if !strings.Contains(got, `class="pill `+c.wantClass+`"`) {
			t.Errorf("statusPill(%q) = %q, want class %q", c.status, got, c.wantClass)
		}
		if !strings.Contains(got, ">"+c.status+"<") {
			t.Errorf("statusPill(%q) = %q, want visible text %q", c.status, got, c.status)
		}
	}
}

func TestStatusPill_UnknownFallsBackToBarePill(t *testing.T) {
	got := string(statusPill("SOME_FUTURE_STATUS"))
	if got != `<span class="pill">SOME_FUTURE_STATUS</span>` {
		t.Errorf("statusPill(unknown) = %q, want bare pill span", got)
	}
}

// TestStatusPill_NamedStringType guards against a real bug caught during P2
// implementation: html/template's function-call type checking is exact, so
// a statusPill(status string) signature panicked at render time on
// order.Order.Status (type order.Status, a named string type) even though
// every other status field this is called on is a plain string — the
// signature was widened to `any` specifically to keep this working.
func TestStatusPill_NamedStringType(t *testing.T) {
	got := string(statusPill(order.StatusPending))
	if !strings.Contains(got, `class="pill pill--pending"`) {
		t.Errorf("statusPill(order.StatusPending) = %q, want pill--pending class", got)
	}
}
