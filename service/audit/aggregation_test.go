package audit

import (
	"testing"
	"time"
)

func TestDenialAggregatorEmitsPowerOfTwoCheckpointsAndSeparatesReasons(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	aggregator := NewDenialAggregator(time.Minute, 2)
	event := Event{
		Actor:   "admin",
		Event:   "ws_origin_rejected",
		IP:      "198.51.100.20",
		Details: map[string]any{"reason": "host_mismatch"},
	}
	wantEmit := []bool{true, true, false, true, false}
	for index, want := range wantEmit {
		emit, count := aggregator.Observe(event, now)
		if emit != want || count != uint64(index+1) {
			t.Fatalf("attempt %d emit=%v count=%d, want %v/%d", index+1, emit, count, want, index+1)
		}
	}
	event.Details["reason"] = "invalid_origin"
	if emit, count := aggregator.Observe(event, now); !emit || count != 1 {
		t.Fatalf("distinct denial reason shared an aggregate: emit=%v count=%d", emit, count)
	}
	now = now.Add(time.Minute)
	event.Details["reason"] = "host_mismatch"
	if emit, count := aggregator.Observe(event, now); !emit || count != 1 {
		t.Fatalf("new aggregation window did not reset: emit=%v count=%d", emit, count)
	}
}

func TestDenialAggregatorBoundsKeys(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	aggregator := NewDenialAggregator(time.Minute, 2)
	for _, ip := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		aggregator.Observe(Event{Event: "login_failed", IP: ip}, now)
		now = now.Add(time.Second)
	}
	if len(aggregator.entries) != 2 {
		t.Fatalf("aggregation keys=%d, want bounded size 2", len(aggregator.entries))
	}
}

func TestDenialAggregatorSeparatesResources(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	aggregator := NewDenialAggregator(time.Minute, 2)
	first := Event{Event: "scope_denied", Actor: "admin", Resource: "settings", IP: "192.0.2.1"}
	second := first
	second.Resource = "backup"

	if emit, _ := aggregator.Observe(first, now); !emit {
		t.Fatal("first resource should emit")
	}
	if emit, _ := aggregator.Observe(second, now); !emit {
		t.Fatal("a distinct protected resource must not be suppressed")
	}
}
