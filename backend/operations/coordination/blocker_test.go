package operationcoordination

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSerializeAdmissionAllowsOnlyOneAdmissionCycle(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			if err := SerializeAdmission(func() error {
				current := active.Add(1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				active.Add(-1)
				return nil
			}); err != nil {
				t.Errorf("SerializeAdmission() error = %v", err)
			}
		}()
	}
	workers.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("admission cycles overlapped: max active = %d", got)
	}
}

func TestTryAdmissionSharesTheExistingFenceAndReleasesAfterReturn(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = SerializeAdmission(func() error { close(entered); <-release; return nil })
	}()
	<-entered
	called := false
	err := TrySerializeAdmission(func() error { called = true; return nil })
	close(release)
	<-done
	if !errors.Is(err, ErrAdmissionBusy) || called {
		t.Fatalf("busy admission called commit: %v %v", err, called)
	}
	if err := TrySerializeAdmission(func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("released admission: %v", err)
	}
	if err := TrySerializeAdmission(nil); err == nil {
		t.Fatal("nil admission callback accepted")
	}
}

func TestSerializeAdmissionRejectsMissingCallback(t *testing.T) {
	if err := SerializeAdmission(nil); err == nil {
		t.Fatal("missing admission callback was accepted")
	}
}
