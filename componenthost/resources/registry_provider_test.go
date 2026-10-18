package resources

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderCanceledReadRetainsCapacityUntilActualReturn(t *testing.T) {
	waitForDrain := func() {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for len(resourceProviderCalls) != 0 {
			select {
			case <-deadline:
				t.Fatal("provider workers did not drain")
			case <-time.After(time.Millisecond):
			}
		}
	}
	// The earlier blocking-contributor deadline test may still own a slot.
	// Wait for its actual return before occupying the complete shared budget.
	waitForDrain()
	release := make(chan struct{})
	defer func() {
		close(release)
		waitForDrain()
	}()
	var calls atomic.Int32
	for range maxContributors {
		ctx, cancel := context.WithCancel(t.Context())
		// Cancel only after entry: a short timer could legitimately expire
		// before admission on a busy coverage/race runner. Registry deadline
		// delivery is independently covered by the blocking-contributor test.
		_, err := callResourceProvider(ctx, time.Hour, func(context.Context) (int, error) {
			calls.Add(1)
			cancel()
			<-release
			return 42, nil
		})
		cancel()
		if err == nil {
			t.Fatal("uncooperative provider escaped cancellation")
		}
	}
	if _, err := callResourceProvider(t.Context(), time.Second, func(context.Context) (int, error) {
		calls.Add(1)
		return 42, nil
	}); err == nil {
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
