package steps

import "testing"

func TestDBVersionMinorIsExactMinor(t *testing.T) {
	tests := []struct {
		version string
		major   int
		minor   int
		want    bool
	}{
		{version: "1.2", major: 1, minor: 2, want: true},
		{version: "1.2.3", major: 1, minor: 2, want: true},
		{version: "1.20.0", major: 1, minor: 2, want: false},
		{version: "2.2.0", major: 1, minor: 2, want: false},
		{version: "not-semver", major: 1, minor: 2, want: false},
	}
	for _, test := range tests {
		if got := dbVersionMinorIs(test.version, test.major, test.minor); got != test.want {
			t.Fatalf("dbVersionMinorIs(%q, %d, %d)=%v, want %v", test.version, test.major, test.minor, got, test.want)
		}
	}
}
