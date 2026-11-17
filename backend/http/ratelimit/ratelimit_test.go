package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestFixedWindowLimitExpiryAndReset(t *testing.T) {
	limiter := NewFixedWindow[string](time.Minute, 2, 8, 0)
	now := time.Unix(1000, 0)
	first := limiter.AllowAt("a", now)
	second := limiter.AllowAt("a", now)
	if !first.Allowed || !second.Allowed {
		t.Fatal("first two requests should be allowed")
	}
	denied := limiter.AllowAt("a", now)
	if denied.Allowed || denied.RetryAfter != time.Minute {
		t.Fatalf("unexpected denied decision: %#v", denied)
	}
	if !limiter.AllowAt("a", now.Add(time.Minute)).Allowed {
		t.Fatal("new window should allow request")
	}
	limiter.Reset("a")
	if !limiter.AllowAt("a", now).Allowed {
		t.Fatal("reset should clear key")
	}
}

func TestFixedWindowConcurrentAndMaxKeys(t *testing.T) {
	limiter := NewFixedWindow[int](time.Minute, 1000, 4, 0)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				limiter.Allow(key)
			}
		}(i)
	}
	wg.Wait()
	if got := limiter.Len(); got > 4 {
		t.Fatalf("len=%d, want <=4", got)
	}
}

func TestFixedWindowEvictsOldestKeyAtCapacity(t *testing.T) {
	limiter := NewFixedWindow[string](time.Minute, 1, 2, 0)
	now := time.Unix(1000, 0)
	limiter.AllowAt("oldest", now)
	limiter.AllowAt("newer", now.Add(time.Second))
	if !limiter.AllowAt("new", now.Add(2*time.Second)).Allowed {
		t.Fatal("new key should be admitted after bounded eviction")
	}
	if !limiter.AllowAt("oldest", now.Add(3*time.Second)).Allowed {
		t.Fatal("oldest key should have been evicted")
	}
}

func TestSlidingWindowRetryAfter(t *testing.T) {
	limiter := NewSlidingWindow[string](time.Minute, 2, 8, 0)
	now := time.Unix(1000, 0)
	limiter.AllowAt("a", now)
	limiter.AllowAt("a", now.Add(10*time.Second))
	denied := limiter.AllowAt("a", now.Add(20*time.Second))
	if denied.Allowed || denied.RetryAfter != 40*time.Second {
		t.Fatalf("unexpected denied decision: %#v", denied)
	}
	if !limiter.AllowAt("a", now.Add(time.Minute)).Allowed {
		t.Fatal("oldest timestamp should expire")
	}
}

func TestFailureWindowBlockTarpitAndExpiry(t *testing.T) {
	limiter := NewFailureWindow[string](time.Minute, 3, time.Minute, 8, 0, time.Second, 2*time.Second)
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		limiter.RecordFailureAt("a", now)
	}
	if limiter.BlockedAt("a", now).Allowed {
		t.Fatal("threshold should block key")
	}
	if delay := limiter.TarpitDelayAt("a", now); delay != time.Second {
		t.Fatalf("delay=%s, want 1s", delay)
	}
	limiter.RecordFailureAt("a", now)
	limiter.RecordFailureAt("a", now)
	if delay := limiter.TarpitDelayAt("a", now); delay != 2*time.Second {
		t.Fatalf("capped delay=%s, want 2s", delay)
	}
	if !limiter.BlockedAt("a", now.Add(2*time.Minute)).Allowed {
		t.Fatal("expired failure window should allow key")
	}
}

func TestThresholdWindowTriggersOncePerWindow(t *testing.T) {
	limiter := NewThresholdWindow[string](time.Minute, 2, 8, 0)
	now := time.Unix(1000, 0)
	if got := limiter.AddAt("a", now); got.Triggered {
		t.Fatal("first event should not trigger")
	}
	if got := limiter.AddAt("a", now); !got.Triggered || got.Count != 2 {
		t.Fatalf("second event=%#v", got)
	}
	if got := limiter.AddAt("a", now); got.Triggered {
		t.Fatal("threshold should trigger once per window")
	}
	if got := limiter.AddAt("a", now.Add(time.Minute)); got.Triggered || got.Count != 1 {
		t.Fatalf("new window=%#v", got)
	}
}

func TestBackgroundGC(t *testing.T) {
	limiter := NewFixedWindow[string](10*time.Millisecond, 1, 8, 5*time.Millisecond)
	t.Cleanup(limiter.Close)
	limiter.Allow("a")
	deadline := time.Now().Add(500 * time.Millisecond)
	for limiter.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if limiter.Len() != 0 {
		t.Fatal("background GC did not prune expired entry")
	}
}
