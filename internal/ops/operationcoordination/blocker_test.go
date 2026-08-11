package operationcoordination

import (
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

func TestSerializeAdmissionRejectsMissingCallback(t *testing.T) {
	if err := SerializeAdmission(nil); err == nil {
		t.Fatal("missing admission callback was accepted")
	}
}
