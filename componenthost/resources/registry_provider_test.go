package resources

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderTimeoutRetainsCapacityUntilActualReturn(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, maxContributors)
	defer func() {
		close(release)
		deadline := time.After(5 * time.Second)
		for len(resourceProviderCalls) != 0 {
			select {
			case <-deadline:
				t.Fatal("provider workers did not drain")
			case <-time.After(time.Millisecond):
			}
		}
	}()
	var calls atomic.Int32
	provider := func(context.Context) (int, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return 42, nil
	}
	for range maxContributors {
		if _, err := callResourceProvider(t.Context(), 2*time.Millisecond, provider); err == nil {
			t.Fatal("uncooperative provider escaped its deadline")
		}
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("provider worker did not start")
		}
	}
	if _, err := callResourceProvider(t.Context(), time.Second, provider); err == nil {
		t.Fatal("exhausted provider capacity admitted another worker")
	}
	if calls.Load() != maxContributors || len(resourceProviderCalls) != maxContributors {
		t.Fatalf("workers=%d slots=%d", calls.Load(), len(resourceProviderCalls))
	}
}

func TestProviderRejectsCanceledLateAndPanickingObservations(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := callResourceProvider(ctx, time.Second, func(context.Context) (int, error) {
		t.Error("pre-canceled provider was invoked")
		return 1, nil
	}); err == nil {
		t.Fatal("canceled read succeeded")
	}
	if _, err := callResourceProvider(t.Context(), time.Second, func(context.Context) (int, error) {
		panic("private provider panic")
	}); err != errProviderCallUnavailable {
		t.Fatalf("provider panic escaped neutral error: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	if value, err := callResourceProvider(ctx, time.Second, func(context.Context) (int, error) {
		cancel()
		return 42, nil
	}); err == nil || value != 0 {
		t.Fatalf("late value accepted: %d %v", value, err)
	}
}
