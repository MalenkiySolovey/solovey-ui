package resourceinventory

import (
	"slices"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

func transportProviderFixture(inboundType, class string, networks []string) (coreinboundcontrol.InboundFallbackSnapshotV1, coreinboundcontrol.CoreRuntimeIdentityV1, coreinboundcontrol.QUICBuildFeatureV1) {
	revision := hostresources.Revision(inboundType + class)
	snapshot := coreinboundcontrol.InboundFallbackSnapshotV1{
		InboundDatabaseID: 1, ResourceID: "core:inbound:1", Tag: "fixture", Type: inboundType,
		ConfigurationRevision: revision,
		Effective:             coreinboundcontrol.EffectiveInboundV1{RuntimeAvailable: true, Present: true, Type: inboundType, Tag: "fixture", Revision: revision, ConfigurationProven: true},
		Authentication:        coreinboundcontrol.AuthenticationShapeV1{Known: true, Expected: true, Count: 1, Revision: revision},
		TLS:                   coreinboundcontrol.TLSShapeV1{Referenced: true, Enabled: true},
		UDPTransport:          coreinboundcontrol.UDPTransportShapeV2{EffectiveNetworks: networks, EffectiveNetworkRevision: revision, Class: class, TransportRevision: revision, SocketIntentRevision: revision, UDPTimeoutRevision: revision, DirectSocketActionable: true},
	}
	identity := coreinboundcontrol.CoreRuntimeIdentityV1{IdentityRevision: revision}
	quic := coreinboundcontrol.QUICBuildFeatureV1{State: coreinboundcontrol.BuildFeatureSupported, SourceRevision: revision, ModuleRevision: revision, BuildProfileRevision: revision, ObservationMethod: "compile_tag"}
	return snapshot, identity, quic
}

func TestTransportProviderU2H2IsUnsupportedWithoutPinnedRegistryProof(t *testing.T) {
	snapshot, identity, quic := transportProviderFixture("h2", "QUIC_NATIVE", []string{"udp"})
	fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
	if fact.StrategyClass != hostresources.TransportUnsupported || fact.ActionableDirectUDPSocket || !slices.Contains(fact.ReasonCodes, "UNSUPPORTED_NO_PINNED_H2_INBOUND") {
		t.Fatalf("unexpected H2 disposition: %#v", fact)
	}
}

func TestTransportProviderU8AssociationNeverClaimsPublicUDPListener(t *testing.T) {
	for _, inboundType := range []string{"socks", "mixed"} {
		snapshot, identity, quic := transportProviderFixture(inboundType, "PROXY_UDP_ASSOCIATION", []string{"tcp"})
		snapshot.UDPTransport.DependentAssociation = true
		fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
		if fact.StrategyClass != hostresources.TransportProxyUDPAssociation || fact.ActionableDirectUDPSocket || slices.Contains(fact.EffectiveNetworks, hostresources.NetworkUDP) {
			t.Fatalf("%s escaped association boundary: %#v", inboundType, fact)
		}
	}
}

func TestTransportProviderU9Port53DoesNotInferDNSService(t *testing.T) {
	snapshot, identity, quic := transportProviderFixture("direct", "PLAIN_UDP", []string{"udp"})
	snapshot.Listener.Port = 53
	fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
	if fact.StrategyClass == hostresources.TransportDNSServiceUnknown || fact.InboundType != "direct" {
		t.Fatalf("port-only DNS inference occurred: %#v", fact)
	}
}

func TestTransportProviderQUICRequiresExactBuildAttestation(t *testing.T) {
	snapshot, identity, quic := transportProviderFixture("tuic", "QUIC_NATIVE", []string{"udp"})
	quic.State = coreinboundcontrol.BuildFeatureUnavailable
	fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
	if fact.ActionableDirectUDPSocket || fact.BuildFeature.State != hostresources.RuntimeFeatureUnavailable || !slices.Contains(fact.ReasonCodes, "BLOCKED_BUILD_FEATURE") {
		t.Fatalf("unattested QUIC became actionable: %#v", fact)
	}
}

func TestTransportProviderSliceOneActionabilityMatchesOwnedHealthProviders(t *testing.T) {
	for _, test := range []struct {
		inbound, class, reason string
		actionable             bool
	}{
		{"shadowsocks", "PLAIN_UDP", "", true},
		{"shadowsocks", "TCP_UDP_DUAL", "", true},
		{"direct", "PLAIN_UDP", "INSPECTION_ONLY_MISSING_REQUEST_RESPONSE_CONTRACT", false},
		{"hysteria", "QUIC_NATIVE", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
		{"hysteria2", "QUIC_NATIVE", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
		{"tuic", "QUIC_NATIVE", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
		{"naive", "QUIC_NATIVE", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
		{"vless", "QUIC_V2RAY_TRANSPORT", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
		{"vmess", "QUIC_V2RAY_TRANSPORT", "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH", false},
	} {
		t.Run(test.inbound+"/"+test.class, func(t *testing.T) {
			snapshot, identity, quic := transportProviderFixture(test.inbound, test.class, []string{"udp"})
			fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
			if fact.ActionableDirectUDPSocket != test.actionable || test.reason != "" && !slices.Contains(fact.ReasonCodes, test.reason) {
				t.Fatalf("actionability mismatch: %#v", fact)
			}
		})
	}
}

func TestTransportProviderTUICKeepsAuthenticationTLSAndQUICOwnershipInCoreFacts(t *testing.T) {
	snapshot, identity, quic := transportProviderFixture("tuic", "QUIC_NATIVE", []string{"udp"})
	snapshot.UDPTransport.ProtocolOwnedZeroRTT = true
	snapshot.UDPTransport.ProtocolOwnedMigration = true
	snapshot.UDPTransport.ProtocolOwnedCID = true
	fact := transportCapabilityV2(snapshot, identity, quic, time.Unix(1000, 0))
	if !fact.AuthenticationPresent || !fact.TLSPresent || !fact.ProtocolOwnedZeroRTT || !fact.ProtocolOwnedMigration || !fact.ProtocolOwnedCID || fact.ActionableDirectUDPSocket {
		t.Fatalf("TUIC ownership escaped core boundary: %#v", fact)
	}
}
