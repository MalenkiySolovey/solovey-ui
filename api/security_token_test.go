package api

import (
	"testing"
	"time"
)

var legacyTokenHeaderSunsetAt = time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)

func withAPITokenNow(t *testing.T, now time.Time) {
	t.Helper()
	previous := apiTokenNow
	apiTokenNow = func() time.Time { return now }
	t.Cleanup(func() { apiTokenNow = previous })
}
