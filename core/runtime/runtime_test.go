package runtime

import (
	"context"
	"errors"
	"testing"

	coretracker "github.com/MalenkiySolovey/solovey-ui/core/tracker"
)

func TestCoreManagersAreUnavailableWhenStopped(t *testing.T) {
	c := NewCore()

	if err := c.withRuntime(func(coreRuntime) error { return nil }); !errors.Is(err, ErrCoreUnavailable) {
		t.Fatalf("stopped runtime error = %v, want %v", err, ErrCoreUnavailable)
	}
	if available, err := c.ConsumeStats(func([]coretracker.Stat) error { return nil }); available || err != nil {
		t.Fatalf("stopped stats result = (%v, %v), want (false, nil)", available, err)
	}
}

func TestConsumeStatsRestoresFailedBatchToOwningTracker(t *testing.T) {
	stats := coretracker.NewStatsTracker()
	stats.RestoreStats([]coretracker.Stat{
		{Resource: "user", Tag: "alice", Direction: false, Traffic: 7},
		{Resource: "user", Tag: "alice", Direction: true, Traffic: 11},
	})
	c := NewCore()
	c.access.Lock()
	c.isRunning = true
	c.statsTracker = stats
	c.access.Unlock()

	sentinel := errors.New("persist failed")
	available, err := c.ConsumeStats(func(samples []coretracker.Stat) error {
		if got := totalTraffic(samples); got != 18 {
			t.Fatalf("consumed traffic = %d, want 18", got)
		}
		return sentinel
	})
	if !available || !errors.Is(err, sentinel) {
		t.Fatalf("failed consumption result = (%v, %v)", available, err)
	}

	available, err = c.ConsumeStats(func(samples []coretracker.Stat) error {
		if got := totalTraffic(samples); got != 18 {
			t.Fatalf("restored traffic = %d, want 18", got)
		}
		return nil
	})
	if !available || err != nil {
		t.Fatalf("restored consumption result = (%v, %v)", available, err)
	}
}

func totalTraffic(samples []coretracker.Stat) int64 {
	var total int64
	for _, sample := range samples {
		total += sample.Traffic
	}
	return total
}

func TestClassifyOutboundCheckErrorUsesStableClasses(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		want string
	}{
		"deadline": {err: context.DeadlineExceeded, want: CheckOutboundErrorTimeout},
		"canceled": {err: context.Canceled, want: CheckOutboundErrorCanceled},
		"internal": {err: errors.New("password=outbound-canary"), want: CheckOutboundErrorFailed},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ClassifyOutboundCheckError(test.err); got != test.want {
				t.Fatalf("class = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCoreCheckOutboundRequiresRunningCore(t *testing.T) {
	c := NewCore()

	result := c.CheckOutbound(context.Background(), "direct", "https://example.com")
	if result.Error != CheckOutboundErrorCoreUnavailable {
		t.Fatalf("expected core unavailable error, got %q", result.Error)
	}
}

func TestNewCoreContextsDoNotInterfere(t *testing.T) {
	type contextKey string
	const key contextKey = "core"
	first := NewCore()
	second := NewCore()

	first.access.Lock()
	first.ctx = context.WithValue(first.ctx, key, "first")
	first.access.Unlock()

	if got := first.ctx.Value(key); got != "first" {
		t.Fatalf("first context value=%v", got)
	}
	if got := second.ctx.Value(key); got != nil {
		t.Fatalf("second core observed first context value: %v", got)
	}
}
