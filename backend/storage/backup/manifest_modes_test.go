package backup

import "testing"

func TestBackupOwnerModesAreClosedAndOwnerSpecific(t *testing.T) {
	for _, test := range []struct {
		owner string
		mode  string
		want  bool
	}{
		{"core", "TYPED", true},
		{"core", "LEGACY_TYPED", true},
		{"core", "OPAQUE_PRESERVED", false},
		{"component", "TYPED", true},
		{"component", "NO_DURABLE_DATA", true},
		{"component", "OPAQUE_PRESERVED", true},
		{"component", "LEGACY_TYPED", false},
		{"component", "FUTURE_MODE", false},
	} {
		if got := validBackupOwnerMode(test.owner, test.mode); got != test.want {
			t.Fatalf("validBackupOwnerMode(%q, %q)=%t, want %t", test.owner, test.mode, got, test.want)
		}
	}
}
