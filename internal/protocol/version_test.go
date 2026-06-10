package protocol

import "testing"

// TestCheckProtocolVersion pins the negotiation policy: refuse on a major
// mismatch, warn (but proceed) for a legacy peer that didn't announce a
// version or a minor-version skew, and accept an exact match silently.
func TestCheckProtocolVersion(t *testing.T) {
	tests := []struct {
		name        string
		major, minor int
		wantRefuse  bool
		wantWarn    bool
	}{
		{"exact match", ProtocolMajor, ProtocolMinor, false, false},
		{"legacy peer (unset)", 0, 0, false, true},
		{"incompatible major (newer)", ProtocolMajor + 1, 0, true, true},
		{"incompatible major (older)", ProtocolMajor + 1, 0, true, true},
		{"minor skew", ProtocolMajor, ProtocolMinor + 5, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refuse, warn := CheckProtocolVersion(tc.major, tc.minor)
			if refuse != tc.wantRefuse {
				t.Fatalf("refuse = %v, want %v", refuse, tc.wantRefuse)
			}
			if (warn != "") != tc.wantWarn {
				t.Fatalf("warn = %q, want non-empty=%v", warn, tc.wantWarn)
			}
		})
	}
}
