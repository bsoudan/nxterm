package server

import (
	"testing"
)

// TestResolveClientWriteChCap covers the three-source precedence:
// env > cfg > default.
func TestResolveClientWriteChCap(t *testing.T) {
	// Ensure no ambient env when running.
	t.Setenv("NXTERMD_WRITE_CH_CAP", "")

	tests := []struct {
		name     string
		env      string
		cfg      int
		want     int
	}{
		{name: "defaults when nothing set", env: "", cfg: 0, want: defaultClientWriteChCap},
		{name: "cfg overrides default", env: "", cfg: 2, want: 2},
		{name: "env overrides cfg", env: "7", cfg: 2, want: 7},
		{name: "env overrides default", env: "4", cfg: 0, want: 4},
		{name: "negative cfg falls through", env: "", cfg: -5, want: defaultClientWriteChCap},
		{name: "zero env falls through to cfg", env: "0", cfg: 3, want: 3},
		{name: "garbage env falls through to cfg", env: "nope", cfg: 3, want: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NXTERMD_WRITE_CH_CAP", tc.env)
			if got := resolveClientWriteChCap(tc.cfg); got != tc.want {
				t.Fatalf("resolveClientWriteChCap(%d) env=%q = %d, want %d",
					tc.cfg, tc.env, got, tc.want)
			}
		})
	}
}
