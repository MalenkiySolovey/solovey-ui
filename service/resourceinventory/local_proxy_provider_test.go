package resourceinventory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCoreLocalProxyProviderFactsLeaseLifecycleAndSharedAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	clock := now
	db := openLocalProxyAuthorityDB(t)
	snapshot := exactLocalProxySnapshotV1("mixed", "127.0.0.1")
	resource := inboundResourceAt(snapshot, now)
	surface := exactLocalProxySurfaceFixture(resource, now)
	provider := &CoreLocalProxyProviderV1{
		db: db, now: func() time.Time { return clock },
		snapshots: func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error) {
			return []coreinboundcontrol.InboundFallbackSnapshotV1{snapshot}, nil
		},
		surfaces: func() hostsurface.Snapshot {
			return hostsurface.Snapshot{GeneratedAt: now.Unix(), Facts: []hostsurface.HostSurfaceFactV1{surface}}
		},
		inventory: func(context.Context) hostresources.ResourceSnapshot {
			return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}
		},
	}
	facts, err := provider.LocalProxyFactsV1(t.Context(), now)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	fact := facts[0]
	if fact.InboundType != "mixed" || fact.Ownership != hostresources.LocalProxyProviderManaged ||
		fact.ListenerState != hostresources.LocalProxyListenerObservedExact ||
		fact.StaticUDPListener || !fact.DependentUDPAssociation ||
		len(fact.Protocols) != 3 {
		t.Fatalf("mixed fact=%#v", fact)
	}
	reference, err := hostresources.ReferenceLocalProxyV1(fact, now)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := provider.LocalProxyFactsV1(t.Context(), now.Add(time.Second))
	if err != nil || refreshed[0].FactRevision != fact.FactRevision {
		t.Fatalf("freshness changed semantic fact: %#v err=%v", refreshed, err)
	}
	lease, err := provider.AcquireLocalProxyGuardLease(t.Context(), hostresources.AcquireLocalProxyGuardLeaseRequestV1{
		RequestID: "acquire-1", HolderID: "operation-1", Purpose: hostresources.LocalProxyGuardPurposeV1,
		ExactReference: reference, FreshnessSeconds: 120,
	})
	if err != nil || lease.State != hostresources.EndpointLeaseReserved {
		t.Fatalf("lease=%#v err=%v", lease, err)
	}
	reopened := &CoreLocalProxyProviderV1{db: db, now: provider.now, snapshots: provider.snapshots, surfaces: provider.surfaces, inventory: provider.inventory}
	current, err := reopened.GetLocalProxyGuardLease(t.Context(), hostresources.GetLocalProxyGuardLeaseRequestV1{LeaseID: lease.LeaseID})
	if err != nil || current.LeaseRevision != lease.LeaseRevision {
		t.Fatalf("reopened authority=%#v err=%v", current, err)
	}
	fenced, err := reopened.FenceLocalProxyGuardLease(t.Context(), hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: "fence-1", LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision,
	})
	if err != nil || fenced.State != hostresources.EndpointLeaseMutationPending {
		t.Fatalf("fenced=%#v err=%v", fenced, err)
	}
	active, err := reopened.ActivateLocalProxyGuardLease(t.Context(), hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: "activate-1", LeaseID: fenced.LeaseID, ExpectedRevision: fenced.LeaseRevision,
	})
	if err != nil || active.State != hostresources.EndpointLeaseActive {
		t.Fatalf("active=%#v err=%v", active, err)
	}
	clock = now.Add(3 * time.Minute)
	released, err := reopened.ReleaseLocalProxyGuardLease(t.Context(), hostresources.ReleaseLocalProxyGuardLeaseRequestV1{
		RequestID: "release-1", LeaseID: active.LeaseID, ExpectedRevision: active.LeaseRevision,
	})
	if err != nil || released.State != hostresources.EndpointLeaseReleased {
		t.Fatalf("expired exact authority was not releasable: released=%#v err=%v", released, err)
	}
}

func TestCoreLocalProxyProviderConflictsWithExistingFrontingAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	db := openLocalProxyAuthorityDB(t)
	snapshot := exactLocalProxySnapshotV1("socks", "127.0.0.1")
	resource := inboundResourceAt(snapshot, now)
	surface := exactLocalProxySurfaceFixture(resource, now)
	provider := &CoreLocalProxyProviderV1{
		db: db, now: func() time.Time { return now },
		snapshots: func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error) {
			return []coreinboundcontrol.InboundFallbackSnapshotV1{snapshot}, nil
		},
		surfaces: func() hostsurface.Snapshot {
			return hostsurface.Snapshot{Facts: []hostsurface.HostSurfaceFactV1{surface}}
		},
		inventory: func(context.Context) hostresources.ResourceSnapshot {
			return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}
		},
	}
	facts, err := provider.LocalProxyFactsV1(t.Context(), now)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	reference, err := hostresources.ReferenceLocalProxyV1(facts[0], now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundEndpointLease{
		LeaseID: "core-fronting-existing", ProviderID: "core", HolderID: "fronting-operation",
		ResourceID: reference.ResourceID, EndpointID: reference.EndpointID, InboundID: reference.InboundDatabaseID,
		State: string(hostresources.EndpointLeaseActive), LeaseRevision: hostresources.Revision("fronting"),
		LeaseJSON: []byte(`{}`), ExactReferenceJSON: []byte(`{}`), LastRequestID: "fronting-acquire",
		IssuedAtUnix: now.Unix(), RenewedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(time.Minute).Unix(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := provider.AcquireLocalProxyGuardLease(t.Context(), hostresources.AcquireLocalProxyGuardLeaseRequestV1{
		RequestID: "local-acquire", HolderID: "local-operation", Purpose: hostresources.LocalProxyGuardPurposeV1,
		ExactReference: reference, FreshnessSeconds: 60,
	}); err == nil {
		t.Fatal("local proxy authority duplicated an existing fronting authority")
	}
}

func openLocalProxyAuthorityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.InboundEndpointLease{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func exactLocalProxySnapshotV1(inboundType, bind string) coreinboundcontrol.InboundFallbackSnapshotV1 {
	digest := strings.Repeat("a", 64)
	protocols := []string{"SOCKS5"}
	if inboundType == "mixed" {
		protocols = []string{"HTTP_CONNECT", "HTTP_FORWARD", "SOCKS5"}
	}
	return coreinboundcontrol.InboundFallbackSnapshotV1{
		Schema: coreinboundcontrol.InboundSnapshotSchemaV1, InboundDatabaseID: 71, ResourceID: "core:inbound:71",
		Tag: "local-proxy", Type: inboundType, ConfigurationRevision: digest, RuntimeIdentityRevision: "runtime-owner",
		Listener:       coreinboundcontrol.ListenerShapeV1{Network: "tcp_udp", AddressFamily: "ipv4", Bind: bind, Port: 1080},
		Authentication: coreinboundcontrol.AuthenticationShapeV1{Known: true, Expected: false, Count: 0, Revision: hostresources.Revision("auth")},
		LocalProxy: coreinboundcontrol.LocalProxyShapeV1{
			Candidate: true, Protocols: protocols, ProtocolRevision: hostresources.Revision("protocol"),
			Authentication: coreinboundcontrol.AuthenticationShapeV1{Known: true, Revision: hostresources.Revision("auth")},
			TLSRevision:    hostresources.Revision("tls"), SystemProxyKnown: true,
			SystemProxyRevision: hostresources.Revision("system"), DependentUDPAssociation: true,
		},
		Effective: coreinboundcontrol.EffectiveInboundV1{
			RuntimeAvailable: true, Present: true, Type: inboundType, Tag: "local-proxy",
			Revision: hostresources.Revision("runtime"), ConfigurationProven: true,
		},
		ReasonCodes: []coreinboundcontrol.ReasonCode{},
	}
}

func exactLocalProxySurfaceFixture(resource hostresources.ProtectableResource, now time.Time) hostsurface.HostSurfaceFactV1 {
	endpoint := resource.Endpoints[0]
	hexA, hexB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	pid, parent, session, uid, gid := 100, 1, 100, 0, 0
	process := hostsurface.ProcessFact{
		PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "1000",
		ExeDigest: hexA, Executable: "/usr/local/bin/solovey-ui",
		ExeDevice: 1, ExeInode: 2, UID: &uid, GID: &gid, ControlGroup: "/system.slice/solovey-ui.service",
	}
	service := hostsurface.ServiceFact{
		SystemdUnit: "solovey-ui.service", MainPID: &pid, FragmentPath: "/etc/systemd/system/solovey-ui.service",
		FragmentSHA256: hexB, ActiveState: "active", SubState: "running",
		ControlGroup: process.ControlGroup, StartMonotonicUsec: 100,
	}
	owner := hostsurface.ListenerOwnerFactV1{
		Schema: hostsurface.ListenerOwnerFactSchemaV1,
		Socket: hostsurface.ListenerSocketIdentityV1{
			Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv4, Bind: endpoint.Key.BindAddress,
			Port: endpoint.Key.Port, Inode: "100", Cookie: 101, CoverageFamilies: []hostsurface.Family{hostsurface.FamilyIPv4},
		},
		Process: process, Service: service,
		Application: hostsurface.ListenerApplicationIdentityV1{
			InstanceID: "fixture-instance", SourceRevision: "src-" + hexA,
			ArtifactRevision: "art-" + hexA, DeploymentID: "dep-" + hexA,
			OwnerContractRevision: hexA, RuntimeRootBindingRevision: hexB,
			ExpectedExecutableSHA256: hexA, ServiceIdentity: "solovey-ui.service",
			ResourceID: resource.ID, ResourceOwnerRevision: resource.Capabilities.OwnerRevision,
			ConfigurationRevision: resource.Capabilities.ConfigRevision,
		},
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}
	owner.Seal()
	return hostsurface.HostSurfaceFactV1{
		Schema: hostsurface.SchemaV1, ID: "surface:" + resource.ID, Network: hostsurface.NetworkTCP,
		Family: hostsurface.FamilyIPv4, Bind: endpoint.Key.BindAddress, Port: endpoint.Key.Port,
		Exposure: hostsurface.ExposureLocal, SocketInode: "100", SocketCookie: 101,
		Process: process, Service: service, ListenerOwner: &owner, RegisteredResourceID: resource.ID,
		DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged,
		FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
		Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: resource.Capabilities.ConfigRevision,
		Classification: hostsurface.ClassificationManagedExact,
	}
}

func TestCoreLocalProxyLeaseJSONContainsNoCredentialsOrDestinations(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	snapshot := exactLocalProxySnapshotV1("socks", "127.0.0.1")
	resource := inboundResourceAt(snapshot, now)
	surface := exactLocalProxySurfaceFixture(resource, now)
	db := openLocalProxyAuthorityDB(t)
	provider := &CoreLocalProxyProviderV1{
		db: db, now: func() time.Time { return now },
		snapshots: func(context.Context, int) ([]coreinboundcontrol.InboundFallbackSnapshotV1, error) {
			return []coreinboundcontrol.InboundFallbackSnapshotV1{snapshot}, nil
		},
		surfaces: func() hostsurface.Snapshot {
			return hostsurface.Snapshot{Facts: []hostsurface.HostSurfaceFactV1{surface}}
		},
		inventory: func(context.Context) hostresources.ResourceSnapshot {
			return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}
		},
	}
	facts, _ := provider.LocalProxyFactsV1(t.Context(), now)
	reference, _ := hostresources.ReferenceLocalProxyV1(facts[0], now)
	lease, err := provider.AcquireLocalProxyGuardLease(t.Context(), hostresources.AcquireLocalProxyGuardLeaseRequestV1{
		RequestID: "acquire-secret-check", HolderID: "operation-secret-check", Purpose: hostresources.LocalProxyGuardPurposeV1,
		ExactReference: reference, FreshnessSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(lease)
	for _, forbidden := range []string{"username", "password", "authorization", "destination", "configuredBind", "configuredPort", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(string(payload)), strings.ToLower(forbidden)) {
			t.Fatalf("lease contains forbidden data %q: %s", forbidden, payload)
		}
	}
}
