package resources

import (
	"slices"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestFirewallBaselineAddressFamilyResolutionCases1Through8(t *testing.T) {
	now := time.Unix(5_000, 0).UTC()
	t.Run("01_explicit_IPv4_claim", func(t *testing.T) {
		resource := graphResource("fixture:family:v4", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{graphSurface(resource, now)}, now, hostresources.AddressFamilyIPv4)
	})
	t.Run("02_explicit_IPv6_claim", func(t *testing.T) {
		resource := graphResource("fixture:family:v6", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "2001:db8::10", 443)
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{graphSurface(resource, now)}, now, hostresources.AddressFamilyIPv6)
	})
	t.Run("03_exact_dual_stack_claim", func(t *testing.T) {
		resource := graphResource("fixture:family:dual", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "::", 443)
		resource.Listen, resource.ListenIntent = "*", hostresources.ConfiguredListenIntentV1{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentDualStack, Network: hostresources.NetworkTCP, Address: "*", Port: 443, RequiredFamilies: []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6}, ConfigurationRevision: resource.Capabilities.ConfigRevision}
		surface := graphSurface(resource, now)
		setOwnerSocket(&surface, hostsurface.FamilyIPv6, "::", false, []hostsurface.Family{hostsurface.FamilyIPv4, hostsurface.FamilyIPv6})
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{surface}, now, hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6)
	})
	t.Run("04_wildcard_proven_IPv4_only", func(t *testing.T) {
		resource := graphResource("fixture:family:wild4", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
		if !slices.Equal(resource.ListenIntent.RequiredFamilies, []hostresources.AddressFamily{hostresources.AddressFamilyIPv4}) {
			t.Fatalf("IPv4 wildcard lost its configured family: %#v", resource.ListenIntent)
		}
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{graphSurface(resource, now)}, now, hostresources.AddressFamilyIPv4)
	})
	t.Run("05_wildcard_proven_IPv6_only", func(t *testing.T) {
		resource := graphResource("fixture:family:wild6", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "::", 443)
		if !slices.Equal(resource.ListenIntent.RequiredFamilies, []hostresources.AddressFamily{hostresources.AddressFamilyIPv6}) {
			t.Fatalf("IPv6 wildcard lost its configured family: %#v", resource.ListenIntent)
		}
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{graphSurface(resource, now)}, now, hostresources.AddressFamilyIPv6)
	})
	t.Run("06_wildcard_separate_IPv4_and_IPv6", func(t *testing.T) {
		resource := graphResource("fixture:family:separate", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
		resource.Listen = "*"
		resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
		v4 := graphSurface(resource, now)
		v6 := graphSurface(resource, now)
		v6.ID += ":v6"
		setOwnerSocket(&v6, hostsurface.FamilyIPv6, "::", true, []hostsurface.Family{hostsurface.FamilyIPv6})
		assertActionableFamilies(t, resource, []hostsurface.HostSurfaceFactV1{v4, v6}, now, hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6)
	})
	t.Run("07_unresolved_wildcard_blocked", func(t *testing.T) {
		resource := graphResource("fixture:family:unresolved", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "endpoint_address_family_unresolved") {
			t.Fatalf("unresolved wildcard was actionable: %#v", graph.Nodes[0])
		}
	})
	t.Run("08_family_change_after_preview_is_stale", func(t *testing.T) {
		resource := graphResource("fixture:family:fence", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
		beforeSurface := graphSurface(resource, now)
		before := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{beforeSurface}, Now: now})
		afterSurface := graphSurface(resource, now.Add(time.Second))
		setOwnerSocket(&afterSurface, hostsurface.FamilyIPv6, "::", true, []hostsurface.Family{hostsurface.FamilyIPv6})
		after := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{afterSurface}, Now: now.Add(time.Second)})
		if before.Revision == after.Revision || before.OwnerObservationRevision == after.OwnerObservationRevision {
			t.Fatal("address-family replacement retained the frozen owner fence")
		}
	})
}

func TestFirewallBaselineGraphAndTOCTOUCases22Through32(t *testing.T) {
	now := time.Unix(6_000, 0).UTC()
	resource := graphResource("fixture:graph:one", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.20", 443)
	exact := graphSurface(resource, now)
	t.Run("22_exact_family_and_MANAGED_EXACT_actionable", func(t *testing.T) {
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{exact}, Now: now})
		if graph.ApplyBlocked || len(graph.Nodes[0].DesiredClaims) != 1 || graph.Nodes[0].DesiredClaims[0].Ambiguous {
			t.Fatalf("exact graph claim is not actionable: %#v", graph.Nodes[0])
		}
	})
	t.Run("23_unknown_family_resource_apply_blocked", func(t *testing.T) {
		unknown := resource
		unknown.Listen, unknown.ListenIntent = "*", hostresources.ConfiguredListenIntentV1{}
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{unknown}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.ReasonCodes, "resource_apply_blocked") {
			t.Fatal("unknown family was not resource_apply_blocked")
		}
	})
	t.Run("24_unknown_owner_resource_apply_blocked", func(t *testing.T) {
		surface := exact
		surface.Classification, surface.ListenerOwner = hostsurface.ClassificationUnknownOwner, nil
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.ReasonCodes, "resource_apply_blocked") {
			t.Fatal("unknown owner was not resource_apply_blocked")
		}
	})
	t.Run("25_foreign_owner_blocked", func(t *testing.T) {
		surface := exact
		surface.Classification, surface.ListenerOwner = hostsurface.ClassificationForeign, nil
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "listener_owner_foreign") {
			t.Fatal("foreign owner was accepted")
		}
	})
	t.Run("26_collision_blocked", func(t *testing.T) {
		other := graphResource("fixture:graph:other", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.20", 443)
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource, other}, Surfaces: []hostsurface.HostSurfaceFactV1{exact, graphSurface(other, now)}, Now: now})
		if !graph.ApplyBlocked || len(graph.Collisions) != 1 {
			t.Fatal("exact socket collision was accepted")
		}
	})
	t.Run("27_incomplete_dual_stack_blocked", func(t *testing.T) {
		dual := resource
		dual.Listen, dual.ListenIntent = "*", hostresources.ConfiguredListenIntentV1{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentDualStack, Network: hostresources.NetworkTCP, Address: "*", Port: 443, RequiredFamilies: []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6}, ConfigurationRevision: dual.Capabilities.ConfigRevision}
		surface := graphSurface(dual, now)
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{dual}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "endpoint_address_family_unresolved") {
			t.Fatal("partial dual-stack coverage was accepted")
		}
	})
	t.Run("29_owner_change_before_prepare_rejected", func(t *testing.T) {
		changed := exact
		owner := *changed.ListenerOwner
		owner.Process.StartTime = "6001"
		owner.Seal()
		changed.ListenerOwner = &owner
		assertFenceChanged(t, resource, exact, changed, now)
	})
	t.Run("30_deployment_change_before_prepare_rejected", func(t *testing.T) {
		changed := exact
		owner := *changed.ListenerOwner
		owner.Application.DeploymentID = "dep-" + strings.Repeat("7", 64)
		owner.Seal()
		changed.ListenerOwner = &owner
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{changed}, Now: now})
		if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "listener_deployment_mismatch") {
			t.Fatal("deployment replacement was accepted")
		}
	})
	t.Run("31_configuration_revision_change_rejected", func(t *testing.T) {
		changed := exact
		owner := *changed.ListenerOwner
		owner.Application.ConfigurationRevision = strings.Repeat("7", 64)
		owner.Seal()
		changed.ListenerOwner = &owner
		graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{changed}, Now: now})
		if !graph.ApplyBlocked {
			t.Fatal("configuration replacement was accepted")
		}
	})
	t.Run("32_refreshed_observation_invalidates_old_preview", func(t *testing.T) {
		refreshed := exact
		owner := *refreshed.ListenerOwner
		owner.Socket.Cookie++
		owner.ObservedAt, owner.ExpiresAt = now.Add(time.Second).Unix(), now.Add(31*time.Second).Unix()
		owner.Seal()
		refreshed.ListenerOwner = &owner
		refreshed.LastSeen, refreshed.ExpiresAt = owner.ObservedAt, owner.ExpiresAt
		assertFenceChanged(t, resource, exact, refreshed, now)
	})
}

func assertActionableFamilies(t *testing.T, resource hostresources.ProtectableResource, surfaces []hostsurface.HostSurfaceFactV1, now time.Time, want ...hostresources.AddressFamily) {
	t.Helper()
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: surfaces, Now: now})
	if graph.ApplyBlocked || len(graph.Nodes) != 1 || len(graph.Nodes[0].DesiredClaims) != len(want) {
		t.Fatalf("resolved graph is not actionable: %#v", graph)
	}
	families := make([]hostresources.AddressFamily, 0, len(graph.Nodes[0].DesiredClaims))
	for _, claim := range graph.Nodes[0].DesiredClaims {
		families = append(families, claim.Key.AddressFamily)
	}
	for _, family := range want {
		if !slices.Contains(families, family) {
			t.Fatalf("resolved families %v omit %s", families, family)
		}
	}
}

func setOwnerSocket(surface *hostsurface.HostSurfaceFactV1, family hostsurface.Family, bind string, ipv6Only bool, coverage []hostsurface.Family) {
	owner := *surface.ListenerOwner
	owner.Socket.Family, owner.Socket.Bind, owner.Socket.CoverageFamilies = family, bind, append([]hostsurface.Family(nil), coverage...)
	owner.Socket.Wildcard = hostresources.NormalizeListen(bind).Wildcard()
	if family == hostsurface.FamilyIPv6 {
		owner.Socket.IPv6Only = &ipv6Only
	} else {
		owner.Socket.IPv6Only = nil
	}
	owner.Seal()
	surface.ListenerOwner, surface.Network, surface.Family, surface.Bind = &owner, owner.Socket.Network, family, bind
	surface.SocketInode, surface.SocketCookie = owner.Socket.Inode, owner.Socket.Cookie
}

func assertFenceChanged(t *testing.T, resource hostresources.ProtectableResource, beforeSurface, afterSurface hostsurface.HostSurfaceFactV1, now time.Time) {
	t.Helper()
	before := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{beforeSurface}, Now: now})
	after := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{afterSurface}, Now: now})
	if before.Revision == after.Revision || before.OwnerObservationRevision == after.OwnerObservationRevision {
		t.Fatal("owner change did not invalidate graph/observation fences")
	}
}
