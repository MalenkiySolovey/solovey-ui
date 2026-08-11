//go:build phase15_performance

package fronting

import (
	"context"
	"sort"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestIsolatedSlice1PlannerV2WallClockBudget(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := largePlanInputV2(t, now)
	durations := make([]time.Duration, 40)
	for index := range durations {
		started := time.Now()
		plan, err := PlanFrontingStrategyV2(input)
		durations[index] = time.Since(started)
		if err != nil || plan.Strategy.Selected != StrategySNIPreread {
			t.Fatalf("large plan failed: selected=%s err=%v", plan.Strategy.Selected, err)
		}
	}
	assertP95BelowV2(t, durations, 250*time.Millisecond)
}

func TestIsolatedSlice2FixedL4CandidateV2WallClockBudget(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	durations := make([]time.Duration, 1000)
	for index := range durations {
		started := time.Now()
		if _, err := RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now); err != nil {
			t.Fatal(err)
		}
		durations[index] = time.Since(started)
	}
	assertP95BelowV2(t, durations, 10*time.Millisecond)
}

func TestIsolatedSlice2FixedL4WorkflowV2WallClockBudget(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-workflow-isolated-performance")
	input := ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest, Confirmation: "APPLY FRONTING " + prepared.OperationID}
	if result, err := fixture.workflow.ApplyV2(context.Background(), input); err != nil || result.State != protectionoperations.StateApplied {
		t.Fatalf("initial apply result=%#v err=%v", result, err)
	}
	durations := make([]time.Duration, 250)
	for index := range durations {
		started := time.Now()
		result, err := fixture.workflow.ApplyV2(context.Background(), input)
		durations[index] = time.Since(started)
		if err != nil || result.State != protectionoperations.StateApplied {
			t.Fatalf("replay=%d result=%#v err=%v", index, result, err)
		}
	}
	assertP95BelowV2(t, durations, 250*time.Millisecond)
}

func TestIsolatedSlice3SNIPrereadCandidateV2WallClockBudget(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	input := largePlanInputV2(t, now)
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || plan.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("maximum plan selected=%s err=%v", plan.Strategy.Selected, err)
	}
	authorities := make(map[string]string, len(plan.Selectors.TargetRevisions))
	for index, revision := range plan.Selectors.TargetRevisions {
		authorities[revision] = v2Revision(struct{ Lease int }{index})
	}
	durations := make([]time.Duration, 31)
	for index := range durations {
		started := time.Now()
		if _, err := RenderSNIPrereadCandidateV2(plan, input, authorities, now); err != nil {
			t.Fatal(err)
		}
		durations[index] = time.Since(started)
	}
	assertP95BelowV2(t, durations, 250*time.Millisecond)
}

func assertP95BelowV2(t *testing.T, durations []time.Duration, threshold time.Duration) {
	t.Helper()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95+99)/100-1]
	if p95 >= threshold {
		t.Fatalf("isolated wall-clock budget p95=%s threshold=%s", p95, threshold)
	}
	t.Logf("isolated wall-clock budget p95=%s threshold=%s", p95, threshold)
}
