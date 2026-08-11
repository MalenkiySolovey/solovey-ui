package fronting

import (
	"context"
	"runtime"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestFixedL4CandidateV2PerformanceInvariants(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	before := runtime.NumGoroutine()
	lastSize := 0
	for range 1000 {
		candidate, err := RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now)
		if err != nil || len(candidate.Bytes) > MaxFutureCandidateBytesV1 {
			t.Fatalf("candidate bytes=%d err=%v", len(candidate.Bytes), err)
		}
		lastSize = len(candidate.Bytes)
	}
	allocations := testing.AllocsPerRun(1000, func() {
		_, _ = RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now)
	})
	runtime.GC()
	after := runtime.NumGoroutine()
	if allocations > 500 || after > before+1 {
		t.Fatalf("fixed L4 invariants allocations=%.0f goroutines=%d->%d", allocations, before, after)
	}
	t.Logf("fixed L4 candidate: bytes=%d allocations=%.0f goroutines=%d->%d", lastSize, allocations, before, after)
}

func TestFixedL4WorkflowV2PerformanceInvariants(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-workflow-performance")
	input := ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest, Confirmation: "APPLY FRONTING " + prepared.OperationID}
	if result, err := fixture.workflow.ApplyV2(context.Background(), input); err != nil || result.State != protectionoperations.StateApplied {
		t.Fatalf("initial apply result=%#v err=%v", result, err)
	}
	before := runtime.NumGoroutine()
	for index := range 250 {
		result, err := fixture.workflow.ApplyV2(context.Background(), input)
		if err != nil || result.State != protectionoperations.StateApplied {
			t.Fatalf("replay=%d result=%#v err=%v", index, result, err)
		}
	}
	allocations := testing.AllocsPerRun(20, func() {
		_, _ = fixture.workflow.ApplyV2(context.Background(), input)
	})
	runtime.GC()
	after := runtime.NumGoroutine()
	if allocations > 10_000 || after > before+1 || fixture.nginx.Reloads != 1 {
		t.Fatalf("workflow invariants allocations=%.0f goroutines=%d->%d reloads=%d", allocations, before, after, fixture.nginx.Reloads)
	}
	t.Logf("fixed L4 workflow fake replay: runs=250 allocations=%.0f goroutines=%d->%d", allocations, before, after)
}
