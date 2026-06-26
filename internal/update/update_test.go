package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.1.1", true},
		{"0.1.0", "v0.1.0", false},
		{"v0.1.0", "0.1.0", false},
		{"v0.2.0", "v0.1.9", false},
		{"v1.0.0", "v1.0.1", true},
		{"v0.9.0", "v0.10.0", true}, // numeric, not lexical
		{"v0.1.0", "v0.1.0-rc1", false},
		{"v0.1.0", "", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
