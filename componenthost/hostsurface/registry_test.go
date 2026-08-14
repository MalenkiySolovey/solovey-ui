package hostsurface

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type failingProvider struct{}

func registerHostProvider(t *testing.T, registry *Registry, provider Provider) func() {
	t.Helper()
	unregister, err := registry.Register(provider)
	if err != nil {
		t.Fatal(err)
	}
	return unregister
}

func (failingProvider) SourceID() string { return "fixture" }
func (failingProvider) Observe(context.Context, Limits) (Observation, error) {
	return Observation{}, errors.New("offline")
}

type unsafeProvider struct{}

func (unsafeProvider) SourceID() string { return "fixture" }
func (unsafeProvider) Observe(context.Context, Limits) (Observation, error) {
	pid := 10
	return Observation{Facts: []HostSurfaceFactV1{{ID: `/secret/id`, Bind: `/secret/socket`, Process: ProcessFact{PID: &pid, StartTime: `/secret/start`, ExeDigest: `/secret/exe`}, Source: `/secret/source`, ReasonCodes: []string{`/secret/reason`}}}}, nil
}

type invalidSourceProvider struct{ unsafeProvider }

func (invalidSourceProvider) SourceID() string { return `/var/lib/private/provider` }

type blockingProvider struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (*blockingProvider) SourceID() string { return "blocking" }
func (p *blockingProvider) Observe(context.Context, Limits) (Observation, error) {
	p.calls.Add(1)
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-p.release
	pid, uid := 10, 1000
	return Observation{Facts: []HostSurfaceFactV1{{Network: NetworkTCP, Family: FamilyIPv4, Bind: "127.0.0.1", Port: 8080, Process: ProcessFact{PID: &pid, UID: &uid}}}}, nil
}

func TestRegistryCoalescesReconciliationAndDeepClonesProcessIdentity(t *testing.T) {
	registry := NewRegistry()
	provider := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{})}
	registerHostProvider(t, registry, provider)
	results := make(chan Snapshot, 2)
	go func() { results <- registry.Reconcile(context.Background()) }()
	<-provider.started
	go func() { results <- registry.Reconcile(context.Background()) }()
	time.Sleep(10 * time.Millisecond)
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("concurrent reconciliation ran provider %d times", calls)
	}
	close(provider.release)
	first, second := <-results, <-results
	if len(first.Facts) != 1 || len(second.Facts) != 1 {
		t.Fatalf("coalesced snapshots = %#v / %#v", first, second)
	}
	*first.Facts[0].Process.PID = 999
	*first.Facts[0].Process.UID = 999
	cached := registry.Snapshot().Facts[0]
	if *cached.Process.PID != 10 || *cached.Process.UID != 1000 {
		t.Fatalf("caller mutated cached process identity: %#v", cached.Process)
	}
}

func TestCloneFactDeepClonesNestedListenerOwnerPointers(t *testing.T) {
	pid, parent, session, uid, gid := 10, 1, 10, 0, 0
	mainPID := 10
	original := HostSurfaceFactV1{ListenerOwner: &ListenerOwnerFactV1{Process: ProcessFact{PID: &pid, ParentPID: &parent, SessionID: &session, UID: &uid, GID: &gid}, Service: ServiceFact{MainPID: &mainPID}}}
	copy := cloneFact(original)
	*copy.ListenerOwner.Process.PID = 99
	*copy.ListenerOwner.Process.ParentPID = 99
	*copy.ListenerOwner.Process.SessionID = 99
	*copy.ListenerOwner.Process.UID = 99
	*copy.ListenerOwner.Process.GID = 99
	*copy.ListenerOwner.Service.MainPID = 99
	if *original.ListenerOwner.Process.PID != 10 || *original.ListenerOwner.Process.ParentPID != 1 || *original.ListenerOwner.Process.SessionID != 10 || *original.ListenerOwner.Process.UID != 0 || *original.ListenerOwner.Process.GID != 0 || *original.ListenerOwner.Service.MainPID != 10 {
		t.Fatal("caller mutated nested cached listener owner identity")
	}
}

type manyFactsProvider struct{ source string }

func (p manyFactsProvider) SourceID() string { return p.source }
func (p manyFactsProvider) Observe(_ context.Context, limits Limits) (Observation, error) {
	facts := make([]HostSurfaceFactV1, limits.MaxSockets)
	for index := range facts {
		facts[index] = HostSurfaceFactV1{Network: NetworkTCP, Family: FamilyIPv4, Bind: "127.0.0.1", Port: uint16(index%65535 + 1)}
	}
	return Observation{Facts: facts}, nil
}

func TestRegistryAppliesSocketCapAcrossProviders(t *testing.T) {
	registry := NewRegistry()
	registerHostProvider(t, registry, manyFactsProvider{source: "a"})
	registerHostProvider(t, registry, manyFactsProvider{source: "b"})
	snapshot := registry.Reconcile(context.Background())
	if len(snapshot.Facts) != DefaultLimits().MaxSockets || !snapshot.Truncated {
		t.Fatalf("global socket cap not enforced: facts=%d truncated=%t", len(snapshot.Facts), snapshot.Truncated)
	}
}

type countedProvider struct {
	source string
	calls  *atomic.Int32
}

func (p countedProvider) SourceID() string { return p.source }
func (p countedProvider) Observe(context.Context, Limits) (Observation, error) {
	p.calls.Add(1)
	return Observation{}, nil
}

func TestRegistryRejectsMultipleProvidersWithTheSameProductionIdentity(t *testing.T) {
	registry := NewRegistry()
	var calls atomic.Int32
	registerHostProvider(t, registry, countedProvider{source: "fixture-protection:linux-hostsurface", calls: &calls})
	if _, err := registry.Register(countedProvider{source: "fixture-protection:linux-hostsurface", calls: &calls}); err == nil {
		t.Fatal("duplicate host-surface authority was accepted")
	}
	snapshot := registry.Reconcile(context.Background())
	if calls.Load() != 1 || len(snapshot.Facts) != 0 {
		t.Fatalf("valid provider was displaced after duplicate rejection: calls=%d snapshot=%#v", calls.Load(), snapshot)
	}
}

type budgetProvider struct {
	requested time.Duration
	observed  time.Duration
}

func (*budgetProvider) SourceID() string                    { return "budget" }
func (p *budgetProvider) ObservationTimeout() time.Duration { return p.requested }
func (p *budgetProvider) Observe(ctx context.Context, limits Limits) (Observation, error) {
	p.observed = limits.Timeout
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 {
		return Observation{}, errors.New("provider deadline missing")
	}
	return Observation{}, nil
}

func TestRegistryAllowsOnlyTheBoundedOwnerObservationBudget(t *testing.T) {
	for _, test := range []struct {
		name      string
		requested time.Duration
		want      time.Duration
	}{
		{name: "production_owner_budget", requested: 80 * time.Second, want: 80 * time.Second},
		{name: "oversized_budget_rejected", requested: 81 * time.Second, want: DefaultLimits().Timeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			provider := &budgetProvider{requested: test.requested}
			registerHostProvider(t, registry, provider)
			_ = registry.Reconcile(context.Background())
			if provider.observed != test.want {
				t.Fatalf("provider budget=%s want=%s", provider.observed, test.want)
			}
		})
	}
}

func TestRegistryDoesNotExposeProviderPathsOrFreeFormIDs(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Register(invalidSourceProvider{}); err == nil {
		t.Fatal("unsafe provider identity was accepted")
	}
	registerHostProvider(t, registry, unsafeProvider{})
	snapshot := registry.Reconcile(context.Background())
	payload, _ := json.Marshal(snapshot)
	if strings.Contains(string(payload), "/secret/") || strings.Contains(string(payload), "/var/lib/private") {
		t.Fatalf("unsafe provider data crossed host-surface boundary: %s", payload)
	}
	if len(snapshot.Facts) != 1 || snapshot.Facts[0].Classification != ClassificationUnknownOwner || snapshot.Facts[0].ConfidenceBP != 0 {
		t.Fatalf("unsafe fact did not fail closed: %#v", snapshot)
	}
}

func TestRegistryProviderAbsenceAndFailureAreExplicitUnknown(t *testing.T) {
	registry := NewRegistry()
	snapshot := registry.Reconcile(context.Background())
	if len(snapshot.Facts) != 1 || snapshot.Facts[0].Classification != ClassificationUnknownOwner || len(snapshot.Facts[0].ReasonCodes) == 0 {
		t.Fatalf("absence snapshot = %#v", snapshot)
	}
	registerHostProvider(t, registry, failingProvider{})
	snapshot = registry.Reconcile(context.Background())
	if len(snapshot.Facts) != 1 || snapshot.Facts[0].Source != "fixture" || len(snapshot.Facts[0].ReasonCodes) == 0 {
		t.Fatalf("failure snapshot = %#v", snapshot)
	}
}

func TestStableIDIgnoresTransientProcessIdentity(t *testing.T) {
	pidOne, pidTwo := 10, 20
	left := HostSurfaceFactV1{Network: NetworkTCP, Family: FamilyIPv4, Bind: "127.0.0.1", Port: 443, Source: "fixture", Process: ProcessFact{PID: &pidOne, StartTime: "100", ExeDigest: strings.Repeat("a", 64)}}
	right := left
	right.Process = ProcessFact{PID: &pidTwo, StartTime: "200", ExeDigest: strings.Repeat("b", 64)}
	if StableID(left) != StableID(right) {
		t.Fatal("stable host-surface ID changed across a process restart")
	}
	right.Source = "other-provider"
	if StableID(left) == StableID(right) {
		t.Fatal("facts from distinct neutral sources shared an ID")
	}
}
