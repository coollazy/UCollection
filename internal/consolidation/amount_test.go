package consolidation

import "testing"

// TestParseTRXAmount 覆蓋第13項：手續費「每筆金額」改為人類可讀 TRX 輸入，
// 後端用字串法轉 int64 最小單位 sun（零浮點，安全鐵律6）。
func TestParseTRXAmount(t *testing.T) {
	ok := []struct {
		in   string
		want int64
	}{
		{"3", 3000000},
		{"3.5", 3500000},
		{"0.000001", 1},
		{"17", 17000000},
		{" 3.5 ", 3500000},
		{".5", 500000},
		{"0.1", 100000},
	}
	for _, c := range ok {
		got, err := parseTRXAmount(c.in)
		if err != nil {
			t.Errorf("parseTRXAmount(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseTRXAmount(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	bad := []string{"", "-1", "0", "3.1234567", "abc", "3.5x"}
	for _, in := range bad {
		if _, err := parseTRXAmount(in); err == nil {
			t.Errorf("parseTRXAmount(%q) expected error, got nil", in)
		}
	}
}
