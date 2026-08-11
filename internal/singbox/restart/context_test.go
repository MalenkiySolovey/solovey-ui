package restart

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunBlockingContextCancelsBeforeOwnership(t *testing.T) {
	manager := NewManager(time.Hour, func() error { return nil })
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- manager.RunBlocking(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	called := false
	err := manager.RunBlockingContext(ctx, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("wait result = %v, called=%v", err, called)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
