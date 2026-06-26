package restart

import (
	"testing"
	"time"
)

func TestManagerDedupesInFlightOperation(t *testing.T) {
	manager := NewManager(time.Hour, func() error { return nil })
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- manager.Run(func() error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started
	ran := false
	if err := manager.Run(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("second operation ran while first operation was in flight")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := manager.Run(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("operation did not run after in-flight operation completed")
	}
}

func TestManagerCancelPendingSighup(t *testing.T) {
	called := make(chan struct{}, 1)
	manager := NewManager(50*time.Millisecond, func() error {
		called <- struct{}{}
		return nil
	})

	if err := manager.SendSighup(); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := manager.Run(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("operation ran while delayed SIGHUP was pending")
	}

	manager.CancelPending()
	if err := manager.Run(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("operation did not run after pending SIGHUP was canceled")
	}

	select {
	case <-called:
		t.Fatal("delayed SIGHUP signal ran after cancellation")
	case <-time.After(75 * time.Millisecond):
	}
}
