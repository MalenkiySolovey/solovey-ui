package restart

import (
	"testing"
	"time"
)

func TestRestartManagerRunBlockingWaitsForInFlightOperation(t *testing.T) {
	manager := NewManager(time.Hour, func() error { return nil })
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- manager.Run(func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	ran := make(chan struct{}, 1)
	blockingDone := make(chan error, 1)
	go func() {
		blockingDone <- manager.RunBlocking(func() error {
			ran <- struct{}{}
			return nil
		})
	}()

	select {
	case <-ran:
		t.Fatal("runBlocking executed while another operation was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-blockingDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("runBlocking operation never ran after the in-flight operation finished")
	}
}

func TestRestartManagerRunBlockingWaitsForPendingRestart(t *testing.T) {
	manager := NewManager(time.Hour, func() error { return nil })
	if err := manager.SendSighup(); err != nil {
		t.Fatal(err)
	}

	ran := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- manager.RunBlocking(func() error {
			close(ran)
			return nil
		})
	}()
	select {
	case <-ran:
		t.Fatal("runBlocking executed while a restart timer was pending")
	case <-time.After(50 * time.Millisecond):
	}

	manager.CancelPending()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("runBlocking operation did not run after the pending restart was canceled")
	}
}
