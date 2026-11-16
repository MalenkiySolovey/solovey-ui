//go:build !minimal

package remote

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type checkStoreFixture struct {
	Store
	active  atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
	entered chan struct{}
}

func (s *checkStoreFixture) ProbeConnection(ctx context.Context, _ RemoteOutboundConnection, _ string) CheckOutboundResult {
	current := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.peak.Load(); current > previous; previous = s.peak.Load() {
		if s.peak.CompareAndSwap(previous, current) {
			break
		}
	}
	s.calls.Add(1)
	s.entered <- struct{}{}
	<-ctx.Done()
	return CheckOutboundResult{Error: s.ClassifyCheckError(ctx.Err())}
}
func (*checkStoreFixture) ClassifyCheckError(err error) string { return err.Error() }
func TestConnectionChecksBoundWorkersAndDrainCanceledQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &checkStoreFixture{entered: make(chan struct{}, checkConcurrency)}
	connections := make([]RemoteOutboundConnection, checkConcurrency*4)
	for i := range connections {
		connections[i] = RemoteOutboundConnection{Id: uint(i + 1), Enabled: true, OutboundTag: fmt.Sprintf("source-%d", i)}
	}
	done := make(chan []CheckResult, 1)
	go func() { done <- CheckConnectionRecordsWithDB(ctx, store, connections, "") }()
	for range checkConcurrency {
		select {
		case <-store.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("bounded workers did not start")
		}
	}
	cancel()
	select {
	case results := <-done:
		if len(results) != len(connections) || store.active.Load() != 0 || store.peak.Load() > checkConcurrency || store.calls.Load() != checkConcurrency {
			t.Fatal("canceled probe queue leaked workers or continued execution")
		}
		for i, result := range results {
			if result.ConnectionId != connections[i].Id || result.OutboundTag != connections[i].OutboundTag || result.Error != context.Canceled.Error() {
				t.Fatalf("result identity/cancellation lost at %d: %#v", i, result)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled probes did not drain")
	}
}
