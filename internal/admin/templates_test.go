package admin

import "testing"

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
