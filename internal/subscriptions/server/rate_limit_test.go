package server

import (
	"fmt"
	"testing"
	"time"
)

func TestCanonicalClientIPUnmapsIPv4MappedIPv6(t *testing.T) {
	if got := CanonicalClientIP("::ffff:198.51.100.10"); got != "198.51.100.10" {
		t.Fatalf("CanonicalClientIP = %q", got)
	}
	if got := CanonicalClientIP("fe80::1%eth0"); got != "" {
		t.Fatalf("zone-scoped address should be rejected, got %q", got)
	}
}

func TestRateLimitGCSweepsExpiredBuckets(t *testing.T) {
	limiter := NewRateLimiter()
	t.Cleanup(limiter.Close)
	now := time.Now()

	limiter.limiter.AllowAt("expired", now.Add(-RateLimitWindow-time.Second))
	limiter.limiter.AllowAt("active", now.Add(-10*time.Second))
	limiter.limiter.PruneAt(now)
	if got := limiter.limiter.Len(); got != 1 {
		t.Fatalf("rate-limit bucket count=%d, want one active bucket", got)
	}
	if decision := limiter.limiter.AllowAt("active", now); decision.Count != 2 {
		t.Fatalf("active bucket was not preserved: %#v", decision)
	}
}

func TestRateLimitBucketCapEvictsOverflow(t *testing.T) {
	limiter := NewRateLimiter()
	t.Cleanup(limiter.Close)
	now := time.Now()

	for i := 0; i < RateLimitMaxKeys+17; i++ {
		limiter.limiter.AllowAt(fmt.Sprintf("198.51.100.%d", i), now)
	}
	count := limiter.limiter.Len()

	if count != RateLimitMaxKeys {
		t.Fatalf("rate-limit bucket count=%d, want %d", count, RateLimitMaxKeys)
	}
}
