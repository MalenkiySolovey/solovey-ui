package nativefallback

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestInventoryManagementReaderRejectsOverlapAndUnknownFacts(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	endpoint := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "127.0.0.1", Port: 8443, ServiceKind: hostresources.ManagementPanel, ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("a", 64)}
	reader := InventoryManagementReader{Now: func() time.Time { return now }, Endpoints: func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint}
	}, Resources: func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }}
	result, err := reader.ResolveIsolation(t.Context(), "core:inbound:17", ManagementEndpointFactsV1{EndpointID: "endpoint", EndpointRevision: strings.Repeat("b", 64), Network: "tcp", AddressFamily: "ipv4", Address: "127.0.0.1", Port: 8443, Local: true, ManagementReachability: "no"})
	if err != nil || result.State != "FORBIDDEN" || result.Revision == "" {
		t.Fatalf("management overlap was not forbidden: %#v err=%v", result, err)
	}
	endpoint.ReasonCodes = []string{"stale"}
	reader.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return []hostresources.ManagementEndpointV1{endpoint}
	}
	result, err = reader.ResolveIsolation(t.Context(), "core:inbound:17", ManagementEndpointFactsV1{EndpointID: "endpoint", EndpointRevision: strings.Repeat("b", 64), Network: "tcp", AddressFamily: "ipv4", Address: "127.0.0.1", Port: 9443, Local: true, ManagementReachability: "no"})
	if err != nil || result.State != "UNKNOWN" {
		t.Fatalf("stale management fact did not fail closed: %#v err=%v", result, err)
	}
}

func TestPlannerThousandFactPerformanceAndNoGoroutineLeak(t *testing.T) {
	planner, request := thousandFactPlanner(t)
	before := runtime.NumGoroutine()
	durations := make([]time.Duration, 20)
	for index := range durations {
		started := time.Now()
		plan, err := planner.Plan(t.Context(), request)
		durations[index] = time.Since(started)
		if err != nil || !plan.Eligible || plan.ActualState != domain.NativeActualNotApplied {
			t.Fatalf("1,000-fact plan failed: eligible=%v actual=%s err=%v", plan.Eligible, plan.ActualState, err)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[18]
	if p95 > 250*time.Millisecond {
		t.Fatalf("1,000-fact p95 %s exceeds 250ms", p95)
	}
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("planner leaked goroutines: before=%d after=%d", before, after)
	}
	if core := planner.Core.(*fakeCoreReader); core.previewCalls != len(durations) {
		t.Fatalf("performance path skipped core preview: calls=%d plans=%d", core.previewCalls, len(durations))
	}
	if targets := planner.Targets.(*fakeTargetReader); targets.calls != len(durations) {
		t.Fatalf("performance path skipped exact target resolution: calls=%d plans=%d", targets.calls, len(durations))
	}
}

func BenchmarkPlanner1000Facts(b *testing.B) {
	planner, request := thousandFactPlanner(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		plan, err := planner.Plan(context.Background(), request)
		if err != nil || !plan.Eligible {
			b.Fatalf("plan failed: eligible=%v err=%v", plan.Eligible, err)
		}
	}
	if core := planner.Core.(*fakeCoreReader); core.previewCalls != b.N {
		b.Fatalf("benchmark skipped core preview: calls=%d plans=%d", core.previewCalls, b.N)
	}
	if targets := planner.Targets.(*fakeTargetReader); targets.calls != b.N {
		b.Fatalf("benchmark skipped exact target resolution: calls=%d plans=%d", targets.calls, b.N)
	}
}

func thousandFactPlanner(t testing.TB) (Planner, PlanRequestV1) {
	t.Helper()
	now := time.Unix(1_900_000_000, 0).UTC()
	identity := exactRuntimeIdentity()
	snapshot := vlessSnapshot(identity)
	target := targetFixture(t, now, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11, neutralfallback.ApplicationProtocolHTTP2}, []string{"decoy.example"})
	reference, err := neutralfallback.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	core := &fakeCoreReader{identity: identity, snapshot: snapshot, previewAt: now.Add(2 * time.Minute)}
	targets := &fakeTargetReader{target: target}
	endpoints := make([]hostresources.ManagementEndpointV1, 1000)
	for index := range endpoints {
		endpoints[index] = hostresources.ManagementEndpointV1{
			Schema: hostresources.ManagementEndpointSchemaV1, ID: fmt.Sprintf("management:fact:%04d", index), Network: hostresources.NetworkTCP,
			Family: hostresources.AddressFamilyIPv4, Bind: "127.0.0.1", Port: uint16(10_000 + index), ServiceKind: hostresources.ManagementOtherAdmin,
			ConfidenceBP: 10000, ObservedAt: now.Unix(), ConfigurationRevision: strings.Repeat("a", 64),
		}
	}
	management := InventoryManagementReader{
		Now:       func() time.Time { return now },
		Endpoints: func(context.Context, time.Time) []hostresources.ManagementEndpointV1 { return endpoints },
		Resources: func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} },
	}
	return Planner{Core: core, Targets: targets, Management: management, Now: func() time.Time { return now }}, requestFor(snapshot, reference)
}
