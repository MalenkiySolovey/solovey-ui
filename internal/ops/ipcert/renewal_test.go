package ipcert

import (
	"testing"
	"time"
)

func TestShouldRenew(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		notAfter time.Time
		want     bool
	}{
		{"zero notAfter renews", time.Time{}, true},
		{"already expired renews", now.Add(-1 * time.Hour), true},
		{"71h remaining renews", now.Add(71 * time.Hour), true},
		{"exactly threshold does not renew", now.Add(RenewThreshold), false},
		{"73h remaining does not renew", now.Add(73 * time.Hour), false},
		{"fresh shortlived cert does not renew", now.Add(160 * time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRenew(tc.notAfter, now); got != tc.want {
				t.Fatalf("ShouldRenew = %v, want %v", got, tc.want)
			}
		})
	}
}
