package hostsurface

import (
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

func TestFirewallBaselineOwnerCases9Through21(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	resource := firewallBaselineHostResource()
	raw := firewallBaselineRawSocket()
	base := firewallBaselineOwnerFact(resource, now)
	classify := func(facts []hostfacts.ListenerOwnerFactV1, available bool, reasons ...string) hostfacts.HostSurfaceFactV1 {
		observation := &protectionhelper.ListenerOwnerObserveResult{Facts: facts, ReasonCodes: reasons, ObservationRevision: strings.Repeat("9", 64)}
		state := ownerObservationState{Available: available, Observation: observation, ReasonCodes: reasons}
		result := NormalizeWithOwners(PlatformSnapshot{Sockets: []RawSocket{raw}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, map[string]ownerObservationState{ownerStateKey(resource.ID, hostresources.NetworkTCP): state}, now)
		if len(result.Facts) != 1 {
			t.Fatalf("facts=%d", len(result.Facts))
		}
		return result.Facts[0]
	}
	notExact := func(t *testing.T, fact hostfacts.HostSurfaceFactV1) {
		t.Helper()
		if fact.Classification == hostfacts.ClassificationManagedExact || fact.OwnershipMode == hostfacts.OwnershipManaged {
			t.Fatalf("inexact owner became managed: %#v", fact)
		}
	}
	t.Run("09_exact_active_service_owner_MANAGED_EXACT", func(t *testing.T) {
		fact := classify([]hostfacts.ListenerOwnerFactV1{base}, true)
		if fact.Classification != hostfacts.ClassificationManagedExact || fact.ListenerOwner == nil {
			t.Fatalf("exact owner classification=%s", fact.Classification)
		}
	})
	t.Run("10_same_port_foreign_process_FOREIGN", func(t *testing.T) {
		fact := classify(nil, true)
		if fact.Classification != hostfacts.ClassificationForeign {
			t.Fatalf("classification=%s", fact.Classification)
		}
	})
	t.Run("11_PID_reuse_rejected", func(t *testing.T) {
		changed := base
		changed.Process.PID = firewallBaselineIntPtr(101)
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("12_process_start_change_rejected", func(t *testing.T) {
		changed := base
		changed.Process.StartTime = "7001"
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("13_executable_path_hash_change_rejected", func(t *testing.T) {
		changed := base
		changed.Process.Executable, changed.Process.ExeDigest = "/opt/foreign/solovey-ui", strings.Repeat("7", 64)
		changed.Application.ExpectedExecutableSHA256 = changed.Process.ExeDigest
		changed.Seal()
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("14_executable_inode_replacement_rejected", func(t *testing.T) {
		changed := base
		changed.Process.ExeInode++
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("15_wrong_systemd_unit_or_cgroup_rejected", func(t *testing.T) {
		changed := base
		changed.Service.SystemdUnit = "foreign.service"
		changed.Process.ControlGroup, changed.Service.ControlGroup = "/system.slice/foreign.service", "/system.slice/foreign.service"
		changed.Seal()
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("16_previous_deployment_owner_rejected", func(t *testing.T) {
		changed := base
		changed.Application.DeploymentID = "dep-" + strings.Repeat("7", 64)
		changed.Seal()
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("17_same_binary_outside_managed_service_rejected", func(t *testing.T) {
		changed := base
		changed.Service.SystemdUnit = "manual.service"
		changed.Process.ControlGroup, changed.Service.ControlGroup = "/user.slice/manual.service", "/user.slice/manual.service"
		changed.Seal()
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
	t.Run("18_multiple_possible_owners_UNKNOWN_OWNER", func(t *testing.T) {
		fact := classify([]hostfacts.ListenerOwnerFactV1{base, base}, true)
		if fact.Classification != hostfacts.ClassificationUnknownOwner {
			t.Fatalf("classification=%s", fact.Classification)
		}
	})
	t.Run("19_missing_helper_capability_blocked", func(t *testing.T) {
		fact := classify(nil, true, "listener_owner_capability_unavailable")
		if fact.Classification != hostfacts.ClassificationUnknownOwner || !containsReason(fact.ReasonCodes, "listener_owner_capability_unavailable") {
			t.Fatalf("capability absence was not preserved: %#v", fact)
		}
	})
	t.Run("20_stale_observation_blocked", func(t *testing.T) {
		stale := base
		stale.ObservedAt, stale.ExpiresAt = now.Add(-time.Minute).Unix(), now.Add(-30*time.Second).Unix()
		stale.Seal()
		fact := classify([]hostfacts.ListenerOwnerFactV1{stale}, true, "listener_owner_stale")
		if fact.Classification != hostfacts.ClassificationStale {
			t.Fatalf("classification=%s", fact.Classification)
		}
	})
	t.Run("21_socket_identity_change_is_conflict", func(t *testing.T) {
		changed := base
		changed.Socket.Inode = "101"
		changed.Socket.Cookie++
		changed.Seal()
		notExact(t, classify([]hostfacts.ListenerOwnerFactV1{changed}, true))
	})
}

func TestConfiguredWildcardFamilyDoesNotCrossMatch(t *testing.T) {
	resource := firewallBaselineHostResource()
	resource.Listen = "0.0.0.0"
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	if socketMatchesIntent(RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "::", Port: 443}, resource.ListenIntent) {
		t.Fatal("IPv4 wildcard intent accepted an IPv6 socket")
	}
	resource.Listen = "::"
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	if socketMatchesIntent(RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 443}, resource.ListenIntent) {
		t.Fatal("IPv6 wildcard intent accepted an IPv4 socket")
	}
}

func firewallBaselineHostResource() hostresources.ProtectableResource {
	expected := hostresources.ExpectedListenerOwnerV1{Schema: hostresources.ExpectedListenerOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64), InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("2", 64), ArtifactRevision: "art-" + strings.Repeat("3", 64), DeploymentID: "dep-" + strings.Repeat("4", 64), RuntimeRootBindingRevision: strings.Repeat("5", 64), ServiceIdentity: "solovey-ui", SystemdUnit: "solovey-ui.service", ServiceFragmentPath: "/etc/systemd/system/solovey-ui.service", ServiceUnitSHA256: strings.Repeat("7", 64), ServiceControlGroup: "/system.slice/solovey-ui.service", ExecutablePath: "/usr/local/solovey-ui/releases/artifact/solovey-ui", ExecutableSHA256: strings.Repeat("6", 64)}
	resource := hostresources.ProtectableResource{ID: "core:panel:web", Kind: "panel_web", Owner: "core", Protocol: "tcp", Listen: "192.0.2.30", Port: 443, Public: true, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: strings.Repeat("b", 64), ConfigRevision: strings.Repeat("c", 64), ExpectedListenerOwner: expected}}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	return resource
}

func firewallBaselineRawSocket() RawSocket {
	return RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "192.0.2.30", Port: 443, Inode: "100", Processes: []RawProcess{{PID: 100, UID: 0, StartTime: "7000", ExecutableToken: "solovey-ui", SystemdUnit: "solovey-ui.service"}}}
}

func firewallBaselineOwnerFact(resource hostresources.ProtectableResource, now time.Time) hostfacts.ListenerOwnerFactV1 {
	expected := resource.Capabilities.ExpectedListenerOwner
	process := hostfacts.ProcessFact{PID: firewallBaselineIntPtr(100), ParentPID: firewallBaselineIntPtr(1), SessionID: firewallBaselineIntPtr(100), StartTime: "7000", ExeDigest: expected.ExecutableSHA256, Executable: "/usr/local/solovey-ui/releases/artifact/solovey-ui", ExeDevice: 1, ExeInode: 2, UID: firewallBaselineIntPtr(0), GID: firewallBaselineIntPtr(0), ControlGroup: "/system.slice/solovey-ui.service"}
	service := hostfacts.ServiceFact{SystemdUnit: expected.SystemdUnit, MainPID: firewallBaselineIntPtr(100), FragmentPath: "/etc/systemd/system/solovey-ui.service", FragmentSHA256: expected.ServiceUnitSHA256, ActiveState: "active", SubState: "running", ControlGroup: process.ControlGroup, StartMonotonicUsec: 100}
	fact := hostfacts.ListenerOwnerFactV1{Schema: hostfacts.ListenerOwnerFactSchemaV1, Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "192.0.2.30", Port: 443, Inode: "100", Cookie: 101, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}}, Process: process, Service: service, Application: hostfacts.ListenerApplicationIdentityV1{InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision, ArtifactRevision: expected.ArtifactRevision, DeploymentID: expected.DeploymentID, OwnerContractRevision: expected.ContractRevision, RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision, ExpectedExecutableSHA256: expected.ExecutableSHA256, ServiceIdentity: expected.ServiceIdentity, ResourceID: resource.ID, ResourceOwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision}, ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix()}
	fact.Seal()
	return fact
}

func firewallBaselineIntPtr(value int) *int { return &value }
