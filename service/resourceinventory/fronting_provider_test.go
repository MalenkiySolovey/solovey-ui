package resourceinventory

import (
	"context"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCoreFrontingProviderLifecycleAndAuthoritySurviveReopen(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil || db.AutoMigrate(&model.InboundEndpointLease{}) != nil {
		t.Fatalf("open/migrate authority DB: %v", err)
	}
	snapshot := exactLocalBackendSnapshotV1()
	provider := &CoreFrontingBackendProviderV1{db: db, now: func() time.Time { return now }, snapshots: func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error) {
		return []coreinboundcontrol.InboundFallbackSnapshotV1{snapshot}, nil
	}}
	registry := hostresources.NewFrontingBackendRegistryV1()
	unregister, err := registry.Register(provider)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := registry.FactsV1(t.Context(), now)
	if err != nil || len(facts) != 1 {
		t.Fatalf("provider facts=%#v err=%v", facts, err)
	}
	reference, err := hostresources.ReferenceFrontingBackendV1(facts[0], hostresources.ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	refreshedFacts, err := registry.FactsV1(t.Context(), now.Add(time.Second))
	if err != nil || len(refreshedFacts) != 1 {
		t.Fatalf("refreshed provider facts=%#v err=%v", refreshedFacts, err)
	}
	refreshedReference, err := hostresources.ReferenceFrontingBackendV1(refreshedFacts[0], hostresources.ProxyModeOff, now.Add(time.Second))
	if err != nil || refreshedReference != reference {
		t.Fatalf("unchanged backend refresh changed exact reference: first=%#v refreshed=%#v err=%v", reference, refreshedReference, err)
	}
	lease, err := provider.AcquireEndpointLease(t.Context(), hostresources.AcquireEndpointLeaseRequestV1{
		RequestID: "acquire-1", HolderID: "fronting-operation-1", Purpose: hostresources.EndpointLeasePurposeL4FrontingV1,
		ExactReference: reference, FreshnessSeconds: 120,
	})
	if err != nil || lease.State != hostresources.EndpointLeaseReserved {
		t.Fatalf("acquire lease=%#v err=%v", lease, err)
	}
	reopened := &CoreFrontingBackendProviderV1{db: db, now: func() time.Time { return now }, snapshots: provider.snapshots}
	current, err := reopened.GetEndpointLease(t.Context(), hostresources.GetEndpointLeaseRequestV1{LeaseID: lease.LeaseID})
	if err != nil || current.LeaseID != lease.LeaseID || current.LeaseRevision != lease.LeaseRevision || current.ExactReference != lease.ExactReference {
		t.Fatalf("reopened authority=%#v err=%v", current, err)
	}
	mirror := lease
	mirror.State = hostresources.EndpointLeaseReleased
	current, _ = reopened.GetEndpointLease(t.Context(), hostresources.GetEndpointLeaseRequestV1{LeaseID: lease.LeaseID})
	if current.State != hostresources.EndpointLeaseReserved {
		t.Fatal("consumer mirror altered provider-owned authority")
	}
	fenced, err := reopened.FenceEndpointLease(t.Context(), hostresources.MutateEndpointLeaseRequestV1{
		RequestID: "fence-1", LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision,
	})
	if err != nil || fenced.State != hostresources.EndpointLeaseMutationPending {
		t.Fatalf("fence lease=%#v err=%v", fenced, err)
	}
	active, err := reopened.ActivateEndpointLease(t.Context(), hostresources.MutateEndpointLeaseRequestV1{
		RequestID: "activate-1", LeaseID: fenced.LeaseID, ExpectedRevision: fenced.LeaseRevision,
	})
	if err != nil || active.State != hostresources.EndpointLeaseActive {
		t.Fatalf("activate lease=%#v err=%v", active, err)
	}
	released, err := reopened.ReleaseEndpointLease(t.Context(), hostresources.ReleaseEndpointLeaseRequestV1{
		RequestID: "release-1", LeaseID: active.LeaseID, ExpectedRevision: active.LeaseRevision,
		DetachmentRevision: hostresources.Revision("detached"),
	})
	if err != nil || released.State != hostresources.EndpointLeaseReleased {
		t.Fatalf("release lease=%#v err=%v", released, err)
	}
	unregister()
	if _, ok := registry.EndpointLeaseProviderV1(provider.ProviderID()); ok {
		t.Fatal("unregistered provider retained lease authority")
	}
	repeatUnregister, err := registry.Register(reopened)
	if err != nil {
		t.Fatalf("provider lifecycle could not restart cleanly: %v", err)
	}
	repeatFacts, err := registry.FactsV1(t.Context(), now)
	if err != nil || len(repeatFacts) != 1 {
		t.Fatalf("restarted provider facts=%#v err=%v", repeatFacts, err)
	}
	repeatUnregister()
	if _, ok := registry.EndpointLeaseProviderV1(provider.ProviderID()); ok {
		t.Fatal("repeated lifecycle leaked provider authority")
	}
}

func TestFrontingBackendAuthorityCannotBeConstructedFromManagementOrArbitraryEndpoint(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	snapshot := exactLocalBackendSnapshotV1()
	resource := inboundResourceAt(snapshot, now)
	endpoint := resource.Endpoints[0]
	base := hostresources.FrontingBackendFactV1{ProviderID: "core", ContributorID: "core", ProviderRevision: "core-v1",
		HealthRevision: hostresources.Revision("health"), CapacityRevision: hostresources.Revision("capacity"),
		Ownership: hostresources.FrontingBackendProviderManaged, AcceptsProxyProtocol: hostresources.CapabilityNo,
		CanReachManagement: hostresources.CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	management := resource
	management.Kind = "panel_web"
	if _, err := hostresources.NewFrontingBackendFactV1(base, management, endpoint); err == nil {
		t.Fatal("management endpoint became ordinary backend authority")
	}
	arbitrary := endpoint
	arbitrary.Key.Port++
	if _, err := hostresources.NewFrontingBackendFactV1(base, resource, arbitrary); err == nil {
		t.Fatal("caller-supplied host/port became backend authority")
	}
}

func exactLocalBackendSnapshotV1() coreinboundcontrol.InboundFallbackSnapshotV1 {
	snapshot := inventorySnapshot(17, "trojan", "tcp", coreinboundcontrol.CapabilitySupported)
	snapshot.Listener.Bind = "127.0.0.1"
	snapshot.Listener.Port = 2443
	snapshot.Effective = coreinboundcontrol.EffectiveInboundV1{
		RuntimeAvailable: true, Present: true, Type: snapshot.Type, Tag: snapshot.Tag,
		Revision: strings.Repeat("e", 64), ConfigurationProven: true,
	}
	return snapshot
}
