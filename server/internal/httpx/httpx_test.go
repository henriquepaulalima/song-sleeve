package httpx

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.2.0", "0.1.9", 1},
		{"0.1.10", "0.1.9", 1},
		{"1.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3-beta", "1.2.3", 0},
		{"", "0.1.0", -1},
		{"abc", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
