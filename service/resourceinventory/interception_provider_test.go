package resourceinventory

import (
	"context"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

func TestCoreInterceptionProviderFactsAndDurableAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	db := openLocalProxyAuthorityDB(t)
	snapshot := exactInterceptionSnapshotV1()
	resource := inboundResourceAt(snapshot, now)
	surface := exactLocalProxySurfaceFixture(resource, now)
	surface.ListenerOwner.Socket.Wildcard = true
	surface.ListenerOwner.Seal()
	provider := &CoreInterceptionProviderV1{
		db: db, now: func() time.Time { return now },
		snapshots: func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error) {
			return []coreinboundcontrol.InboundFallbackSnapshotV1{snapshot}, nil
		},
		surfaces: func() hostsurface.Snapshot {
			return hostsurface.Snapshot{Facts: []hostsurface.HostSurfaceFactV1{surface}}
		},
	}
	facts, err := provider.InterceptionFactsV1(t.Context(), now)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	fact := facts[0]
	if fact.Kind != hostresources.InterceptionRedirectV1 || fact.Network != hostresources.NetworkTCP ||
		fact.Ownership != hostresources.InterceptionProviderManagedV1 ||
		fact.ListenerState != hostresources.InterceptionListenerObservedExactV1 ||
		!fact.OriginalDestinationPreserved || !fact.SourcePreserved || fact.HealthCapabilityReady {
		t.Fatalf("incorrect Redirect fact: %#v", fact)
	}
	reference, err := hostresources.ReferenceInterceptionV1(fact, now)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := provider.InterceptionFactsV1(t.Context(), now.Add(time.Second))
	if err != nil || refreshed[0].FactRevision != fact.FactRevision {
		t.Fatalf("observation time changed semantic fact: %#v err=%v", refreshed, err)
	}
	lease, err := provider.AcquireInterceptionLease(t.Context(), hostresources.AcquireInterceptionLeaseRequestV1{
		RequestID: "acquire-1", HolderID: "operation-1", ExactReference: reference, FreshnessSeconds: 120,
	})
	if err != nil || lease.State != hostresources.EndpointLeaseReserved {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	reopened := &CoreInterceptionProviderV1{
		db: db, now: provider.now, snapshots: provider.snapshots, surfaces: provider.surfaces,
	}
	current, err := reopened.GetInterceptionLease(t.Context(), hostresources.GetInterceptionLeaseRequestV1{LeaseID: lease.LeaseID})
	if err != nil || current.LeaseRevision != lease.LeaseRevision {
		t.Fatalf("reopened lease=%#v err=%v", current, err)
	}
	fenced, err := reopened.FenceInterceptionLease(t.Context(), hostresources.MutateInterceptionLeaseRequestV1{
		RequestID: "fence-1", LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision,
	})
	if err != nil || fenced.State != hostresources.EndpointLeaseMutationPending {
		t.Fatalf("fenced=%#v err=%v", fenced, err)
	}
	active, err := reopened.ActivateInterceptionLease(t.Context(), hostresources.MutateInterceptionLeaseRequestV1{
		RequestID: "activate-1", LeaseID: fenced.LeaseID, ExpectedRevision: fenced.LeaseRevision,
	})
	if err != nil || active.State != hostresources.EndpointLeaseActive {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	released, err := reopened.ReleaseInterceptionLease(t.Context(), hostresources.ReleaseInterceptionLeaseRequestV1{
		RequestID: "release-1", LeaseID: active.LeaseID, ExpectedRevision: active.LeaseRevision,
		DetachmentRevision: hostresources.Revision("detached"),
	})
	if err != nil || released.State != hostresources.EndpointLeaseReleased {
		t.Fatalf("released=%#v err=%v", released, err)
	}
}

func exactInterceptionSnapshotV1() coreinboundcontrol.InboundFallbackSnapshotV1 {
	digest := strings.Repeat("a", 64)
	return coreinboundcontrol.InboundFallbackSnapshotV1{
		Schema: coreinboundcontrol.InboundSnapshotSchemaV1, InboundDatabaseID: 81,
		ResourceID: "core:inbound:81", Tag: "redirect-in", Type: "redirect",
		ConfigurationRevision: digest, RuntimeIdentityRevision: digest,
		Listener: coreinboundcontrol.ListenerShapeV1{
			Network: "tcp", AddressFamily: "ipv4", Bind: "0.0.0.0", Port: 15001,
		},
		Interception: coreinboundcontrol.InterceptionShapeV1{
			Candidate: true, Kind: "redirect", EffectiveNetworks: []string{"tcp"},
			EffectiveNetworksRevision: hostresources.Revision("tcp"), LinuxOnly: true,
			OriginalDestinationMechanism: "SO_ORIGINAL_DST", OriginalDestinationPreserved: true,
			SourcePreserved: true, SemanticRevision: hostresources.Revision("redirect"),
		},
		Effective: coreinboundcontrol.EffectiveInboundV1{
			RuntimeAvailable: true, Present: true, Type: "redirect", Tag: "redirect-in",
			Revision: hostresources.Revision("runtime"), ConfigurationProven: true,
		},
	}
}
