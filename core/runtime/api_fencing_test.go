package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/daemon"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type barrierAPI struct {
	daemon.StartedServiceClient
	entered, release chan struct{}
}

func (b *barrierAPI) SelectOutbound(ctx context.Context, request *daemon.SelectOutboundRequest, options ...grpc.CallOption) (*emptypb.Empty, error) {
	close(b.entered)
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.StartedServiceClient.SelectOutbound(ctx, request, options...)
}

func waitRuntimeBarrier(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime barrier timed out")
	}
}

func TestRuntimeActionLeaseFencesLateResponseAndReplacement(t *testing.T) {
	c := startPrivateFixture(t)
	oldGeneration := c.privateAPI.generation
	barrier := &barrierAPI{StartedServiceClient: c.privateAPI.rpc, entered: make(chan struct{}), release: make(chan struct{})}
	c.privateAPI.rpc = barrier
	action := make(chan error, 1)
	go func() { action <- c.SelectRuntimeGroup(context.Background(), oldGeneration, "choice", "second") }()
	waitRuntimeBarrier(t, barrier.entered)
	replacement := make(chan error, 1)
	go func() {
		if err := c.Stop(); err != nil {
			replacement <- err
			return
		}
		replacement <- c.Start([]byte(privateFixture))
	}()
	// The old RPC can complete only while its generation still owns the lease.
	close(barrier.release)
	if err := <-action; err != nil {
		t.Fatal(err)
	}
	if err := <-replacement; err != nil {
		t.Fatal(err)
	}
	if c.privateAPI.generation == oldGeneration {
		t.Fatal("replacement identity unchanged")
	}
	groups, err := c.Groups(context.Background(), c.privateAPI.generation)
	if err != nil || groups.Groups[0].Selected != "direct" {
		t.Fatal("old selection mutated replacement")
	}
	if err := c.SelectRuntimeGroup(context.Background(), oldGeneration, "choice", "second"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("late old request controlled replacement")
	}
}

func TestRuntimeLogCancellationAndSubscriberLimit(t *testing.T) {
	c := startPrivateFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	go func() {
		done <- c.SubscribeRuntimeLogs(ctx, c.privateAPI.generation, func(RuntimeLogEvent) bool { once.Do(func() { close(entered) }); return true })
	}()
	waitRuntimeBarrier(t, entered)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("orphaned canceled gRPC log stream")
	}
	if c.logSubscribers.Load() != 0 {
		t.Fatal("subscriber lease retained")
	}
	c.logSubscribers.Store(4)
	if err := c.SubscribeRuntimeLogs(context.Background(), c.privateAPI.generation, func(RuntimeLogEvent) bool { return true }); !errors.Is(err, ErrRuntimeLimit) {
		t.Fatal("unbounded subscriptions")
	}
	c.logSubscribers.Store(0)
}

func TestOldLogBatchCannotDispatchIntoReplacement(t *testing.T) {
	c := startPrivateFixture(t)
	old := c.privateAPI
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start([]byte(privateFixture)); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := c.deliverRuntimeLogs(old, &daemon.Log{Messages: []*daemon.Log_Message{{Message: "old generation"}}}, func(RuntimeLogEvent) bool { called = true; return true })
	if !errors.Is(err, ErrStaleGeneration) || called {
		t.Fatal("stale stream callback dispatched into new generation")
	}
}

func TestGenerationBoundProbeUsesControlledEndpointAndLimits(t *testing.T) {
	c := startPrivateFixture(t)
	generation := c.privateAPI.generation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	if result := c.CheckRuntimeOutbound(context.Background(), generation, "direct", server.URL); !result.OK {
		t.Fatalf("controlled probe error=%s", result.Error)
	}
	groups, err := c.Groups(context.Background(), generation)
	if err != nil || groups.Groups[0].Items[0].TestedAt == 0 {
		t.Fatal("manual result not associated with official outbound history")
	}
	if result := c.CheckRuntimeOutbound(context.Background(), "old", "direct", server.URL); result.Error != "stale_generation" {
		t.Fatal("stale probe accepted")
	}
	for range 4 {
		c.probeSlots <- struct{}{}
	}
	if result := c.CheckRuntimeOutbound(context.Background(), generation, "direct", server.URL); result.Error != "runtime_limit_exceeded" {
		t.Fatal("unbounded probe concurrency")
	}
	for range 4 {
		<-c.probeSlots
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := c.CheckRuntimeOutbound(ctx, generation, "direct", server.URL); result.Error != CheckOutboundErrorCanceled {
		t.Fatalf("probe ignored cancellation: %s", result.Error)
	}
}
