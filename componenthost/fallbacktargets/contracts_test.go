package fallbacktargets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type fixtureProvider struct {
	target TargetV1
	err    error
}

type mutableProvider struct{ target TargetV1 }

func (*mutableProvider) ProviderID() string { return "fixture-provider" }
func (p *mutableProvider) ListTargets(context.Context) ([]TargetV1, error) {
	return []TargetV1{p.target}, nil
}

func (f fixtureProvider) ProviderID() string { return "fixture-provider" }
func (f fixtureProvider) ListTargets(context.Context) ([]TargetV1, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []TargetV1{f.target}, nil
}

func readyTarget(now time.Time) TargetV1 {
	return TargetV1{Schema: TargetSchemaV1, Identity: TargetIdentity{ProviderID: "fixture-provider", TargetID: "site:1"}, PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), Endpoint: EndpointCapability{EndpointID: "endpoint:1", Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "127.0.0.1", Port: 8080, TLS: hostresources.CapabilityNo, Local: true, CanReachManagement: hostresources.CapabilityNo}, Readiness: ReadinessReady, ProviderHealthRevision: "health-1", ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Source: "fixture", ConfidenceBP: 10000}
}

func TestLeaseAcquireRenewReleaseAndStale(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	target := readyTarget(now)
	if _, err := registry.Register(fixtureProvider{target: target}); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	manager := LeaseManager{Registry: registry, Store: store, Now: func() time.Time { return now }}
	ref := TargetReferenceV1{ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1"}
	lease, err := manager.Acquire(context.Background(), "decision:1", ref, 30*time.Second)
	if err != nil || lease.State != "ACTIVE" {
		t.Fatalf("acquire=%#v err=%v", lease, err)
	}
	now = now.Add(10 * time.Second)
	renewed, err := manager.Renew(context.Background(), lease.LeaseID, 30*time.Second)
	if err != nil || renewed.ExpiresAt <= lease.ExpiresAt {
		t.Fatalf("renew=%#v err=%v", renewed, err)
	}
	released, err := manager.Release(context.Background(), lease.LeaseID)
	if err != nil || released.State != "RELEASED" {
		t.Fatalf("release=%#v err=%v", released, err)
	}
	if _, err = manager.Renew(context.Background(), lease.LeaseID, time.Minute); err == nil {
		t.Fatal("released lease renewed")
	}
}

func TestMissingStaleAndUnsafeTargetsFailClosed(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	ref := TargetReferenceV1{ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1"}
	for name, target := range map[string]TargetV1{"stale": func() TargetV1 { v := readyTarget(now); v.ExpiresAt = now.Unix(); return v }(), "management": func() TargetV1 {
		v := readyTarget(now)
		v.Endpoint.CanReachManagement = hostresources.CapabilityUnknown
		return v
	}(), "path-id": func() TargetV1 { v := readyTarget(now); v.Identity.TargetID = "../../site"; return v }(), "unresolved-reason": func() TargetV1 {
		v := readyTarget(now)
		v.ReasonCodes = []string{"fallback_target_ambiguous"}
		return v
	}()} {
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry()
			if _, err := registry.Register(fixtureProvider{target: target}); err != nil {
				t.Fatal(err)
			}
			manager := LeaseManager{Registry: registry, Store: NewMemoryStore(), Now: func() time.Time { return now }}
			if _, err := manager.Acquire(context.Background(), "decision:1", ref, time.Minute); err == nil {
				t.Fatal("unsafe target acquired")
			}
		})
	}
	registry := NewRegistry()
	if _, err := registry.Register(fixtureProvider{err: errors.New("offline")}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(context.Background(), ref, now); err == nil {
		t.Fatal("missing provider target resolved")
	}
	registry = NewRegistry()
	pathTarget := readyTarget(now)
	pathTarget.Identity.TargetID = `../../secret/site`
	pathTarget.Source = `/var/lib/private/target.json`
	if _, err := registry.Register(fixtureProvider{target: pathTarget}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(registry.Snapshot(context.Background(), now))
	if strings.Contains(string(payload), "../") || strings.Contains(string(payload), "/var/lib/private") {
		t.Fatalf("invalid target leaked provider paths: %s", payload)
	}
}

func TestRenewMarksPersistedLeaseStaleWhenTargetIdentityChanges(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	provider := &mutableProvider{target: readyTarget(now)}
	registry := NewRegistry()
	if _, err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	manager := LeaseManager{Registry: registry, Store: store, Now: func() time.Time { return now }}
	ref := TargetReferenceV1{ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1"}
	lease, err := manager.Acquire(context.Background(), "decision:1", ref, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	provider.target.PublishRevision = "publish-2"
	provider.target.ContentDigest = strings.Repeat("b", 64)
	if _, err = manager.Renew(context.Background(), lease.LeaseID, time.Minute); err == nil {
		t.Fatal("lease renewed across target revision/digest change")
	}
	stored, err := store.LoadLease(context.Background(), lease.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "STALE" || stored.ExpiresAt != now.Unix() || len(stored.ReasonCodes) != 1 || stored.ReasonCodes[0] != "fallback_target_reference_stale" {
		t.Fatalf("mismatched lease remained current: %#v", stored)
	}
}

func TestMemoryLeaseStoreCapsCardinality(t *testing.T) {
	store := NewMemoryStore()
	for index := 0; index < MaxReferenceLeases; index++ {
		lease := ReferenceLeaseV1{Schema: LeaseSchemaV1, LeaseID: fmt.Sprintf("lease:%d", index), HolderID: fmt.Sprintf("decision:%d", index), ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: 1, RenewedAt: 1, ExpiresAt: 2, State: "ACTIVE"}
		if err := store.SaveLease(context.Background(), lease); err != nil {
			t.Fatalf("save lease %d: %v", index, err)
		}
	}
	overflow := ReferenceLeaseV1{Schema: LeaseSchemaV1, LeaseID: "lease:overflow", HolderID: "decision:overflow", ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: 1, RenewedAt: 1, ExpiresAt: 2, State: "ACTIVE"}
	if err := store.SaveLease(context.Background(), overflow); err == nil {
		t.Fatal("lease store exceeded its hard cardinality cap")
	}
}
