package restart

import (
	"testing"
	"time"
)

func TestRestartManagerExtraScheduleDedupesPendingRun(t *testing.T) {
	called := make(chan struct{}, 2)
	manager := NewManager(time.Hour, func() error {
		called <- struct{}{}
		return nil
	})

	if err := manager.ScheduleRestart(time.Hour); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	firstTimer := manager.pendingTimer
	manager.mu.Unlock()
	if firstTimer == nil {
		t.Fatal("first schedule did not create pending timer")
	}
	if err := manager.ScheduleRestart(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	secondTimer := manager.pendingTimer
	inFlight := manager.inFlight
	manager.mu.Unlock()
	if secondTimer != firstTimer || !inFlight {
		t.Fatalf("second schedule should keep existing pending timer: same=%v inFlight=%v", secondTimer == firstTimer, inFlight)
	}
	manager.CancelPending()

	select {
	case <-called:
		t.Fatal("deduped pending restart should not fire during test")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRestartManagerExtraCancelPendingIsIdempotent(t *testing.T) {
	called := make(chan struct{}, 1)
	manager := NewManager(time.Hour, func() error {
		called <- struct{}{}
		return nil
	})

	if err := manager.ScheduleRestart(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	manager.CancelPending()
	manager.CancelPending()
	if err := manager.Run(func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	select {
	case <-called:
		t.Fatal("canceled restart fired")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestScheduleRestartBlockingWaitsForInFlightOperation(t *testing.T) {
	signaled := make(chan struct{}, 1)
	manager := NewManager(time.Millisecond, func() error {
		signaled <- struct{}{}
		return nil
	})
	started := make(chan struct{})
	release := make(chan struct{})
	operationDone := make(chan error, 1)
	go func() {
		operationDone <- manager.Run(func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	scheduleDone := make(chan error, 1)
	go func() {
		scheduleDone <- manager.ScheduleRestartBlocking(time.Millisecond)
	}()
	select {
	case err := <-scheduleDone:
		t.Fatalf("blocking restart returned while operation was in flight: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-operationDone; err != nil {
		t.Fatal(err)
	}
	if err := <-scheduleDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-signaled:
	case <-time.After(time.Second):
		t.Fatal("restart was not armed after the in-flight operation finished")
	}
}

func TestScheduleRestartBlockingAcceptsExistingPendingRestart(t *testing.T) {
	manager := NewManager(time.Hour, func() error { return nil })
	if err := manager.ScheduleRestart(time.Hour); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	firstTimer := manager.pendingTimer
	manager.mu.Unlock()
	if firstTimer == nil {
		t.Fatal("initial restart was not scheduled")
	}

	if err := manager.ScheduleRestartBlocking(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	secondTimer := manager.pendingTimer
	manager.mu.Unlock()
	if secondTimer != firstTimer {
		t.Fatal("blocking restart replaced an already sufficient pending restart")
	}
	manager.CancelPending()
}
