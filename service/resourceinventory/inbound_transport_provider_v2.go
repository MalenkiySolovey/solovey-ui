package resourceinventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/gorm"
)

type CoreInboundTransportProviderV2 struct {
	db      *gorm.DB
	control *coreinboundcontrol.Service
}

func NewCoreInboundTransportProviderV2(db *gorm.DB, control *coreinboundcontrol.Service) *CoreInboundTransportProviderV2 {
	return &CoreInboundTransportProviderV2{db: db, control: control}
}
func (*CoreInboundTransportProviderV2) ProviderID() string { return "core-sing-box-inbounds-v2" }

func (p *CoreInboundTransportProviderV2) InboundTransportCapabilitiesV2(ctx context.Context, now time.Time) ([]hostresources.InboundTransportCapabilityV2, error) {
	if p == nil || p.db == nil {
		return nil, fmt.Errorf("inbound transport database is unavailable")
	}
	if p.control == nil {
		return nil, fmt.Errorf("inbound transport control is unavailable")
	}
	snapshots, err := p.control.ListSnapshots(ctx, hostresources.MaxInboundTransportFactsV2+1)
	if err != nil {
		return nil, err
	}
	if len(snapshots) > hostresources.MaxInboundTransportFactsV2 {
		return nil, fmt.Errorf("inbound transport cardinality exceeded")
	}
	identity := p.control.Identity(ctx)
	quic := coreinboundcontrol.ReadQUICBuildFeatureV1(identity)
	result := make([]hostresources.InboundTransportCapabilityV2, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, transportCapabilityV2(snapshot, identity, quic, now))
	}
	return result, nil
}

func transportCapabilityV2(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, identity coreinboundcontrol.CoreRuntimeIdentityV1, quic coreinboundcontrol.QUICBuildFeatureV1, now time.Time) hostresources.InboundTransportCapabilityV2 {
	observed := now.UTC()
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	featureState := hostresources.RuntimeFeatureUnknown
	switch quic.State {
	case coreinboundcontrol.BuildFeatureSupported:
		featureState = hostresources.RuntimeFeatureSupported
	case coreinboundcontrol.BuildFeatureUnavailable:
		featureState = hostresources.RuntimeFeatureUnavailable
	}
	featureReasons := make([]string, 0, len(quic.ReasonCodes))
	for _, reason := range quic.ReasonCodes {
		featureReasons = append(featureReasons, string(reason))
	}
	feature := hostresources.FinalizeRuntimeBuildFeatureV1(hostresources.RuntimeBuildFeatureV1{
		Feature: "with_quic", State: featureState, RuntimeIdentity: identity.IdentityRevision,
		SourceRevision: quic.SourceRevision, ModuleRevision: quic.ModuleRevision,
		BuildProfileRevision: quic.BuildProfileRevision, ObservationMethod: quic.ObservationMethod,
		ObservedAt: observed.Unix(), ExpiresAt: observed.Add(hostresources.MaxInboundTransportFreshnessV2).Unix(), ReasonCodes: featureReasons,
	})
	class := hostresources.InboundTransportClass(snapshot.UDPTransport.Class)
	if !knownTransportClass(class) {
		class = hostresources.TransportUnsupported
	}
	networks := make([]hostresources.Network, 0, len(snapshot.UDPTransport.EffectiveNetworks))
	for _, network := range snapshot.UDPTransport.EffectiveNetworks {
		if network == "tcp" {
			networks = append(networks, hostresources.NetworkTCP)
		}
		if network == "udp" {
			networks = append(networks, hostresources.NetworkUDP)
		}
	}
	if len(networks) == 0 {
		networks = []hostresources.Network{hostresources.NetworkTCP}
		class = hostresources.TransportUnsupported
	}
	reasons := make([]string, 0, 16)
	for _, reason := range snapshot.UDPTransport.ReasonCodes {
		reasons = append(reasons, string(reason))
	}
	for _, reason := range snapshot.Effective.ReasonCodes {
		reasons = append(reasons, string(reason))
	}
	actionable := snapshot.UDPTransport.DirectSocketActionable && snapshot.Effective.ConfigurationProven && containsUDP(networks)
	quicRequired := class == hostresources.TransportQUICNative || class == hostresources.TransportQUICV2Ray || strings.EqualFold(snapshot.Type, "naive")
	if quicRequired && feature.State != hostresources.RuntimeFeatureSupported {
		actionable = false
		reasons = append(reasons, "BLOCKED_BUILD_FEATURE")
	}
	if requiresAuth(snapshot.Type) && !snapshot.Authentication.Expected {
		actionable = false
		reasons = append(reasons, "BLOCKED_AUTHENTICATION_ABSENT")
	}
	if requiresTLS(snapshot.Type, class) && !snapshot.TLS.Enabled {
		actionable = false
		reasons = append(reasons, "BLOCKED_TLS_ABSENT")
	}
	if snapshot.Type == "h2" {
		class, actionable = hostresources.TransportUnsupported, false
		reasons = append(reasons, "UNSUPPORTED_NO_PINNED_H2_INBOUND")
	}
	if snapshot.Type == "redirect" || snapshot.Type == "tproxy" || snapshot.Type == "tun" {
		class, actionable = hostresources.TransportInterception, false
		reasons = append(reasons, "OUT_OF_SCOPE_INTERCEPTION")
	}
	if snapshot.Type == "socks" || snapshot.Type == "mixed" {
		class, actionable = hostresources.TransportProxyUDPAssociation, false
		reasons = append(reasons, "DEPENDENT_ASSOCIATION_NOT_LISTENER")
	}
	// Actionability also requires a current owner-owned bounded
	// request/response implementation. The pinned core exposes that safely for
	// Shadowsocks UDP. Direct has no configured responder contract, and the
	// current QUIC inbounds have no secret-contained active probe adapter.
	if actionable && snapshot.Type != "shadowsocks" {
		actionable = false
		if quicRequired {
			reasons = append(reasons, "INSPECTION_ONLY_MISSING_QUIC_PROTOCOL_HEALTH")
		} else {
			reasons = append(reasons, "INSPECTION_ONLY_MISSING_REQUEST_RESPONSE_CONTRACT")
		}
	}
	if !snapshot.Effective.ConfigurationProven {
		reasons = append(reasons, "BLOCKED_EFFECTIVE_CONFIGURATION_UNPROVEN")
	}
	effectiveRevision := snapshot.Effective.Revision
	if effectiveRevision == "" {
		effectiveRevision = hostresources.Revision(snapshot.Effective)
	}
	value := hostresources.InboundTransportCapabilityV2{
		ProviderID: "core-sing-box-inbounds-v2", ContributorID: "core-resource-inventory", ResourceID: snapshot.ResourceID,
		InboundDatabaseID: snapshot.InboundDatabaseID, InboundTag: snapshot.Tag, InboundType: snapshot.Type, StrategyClass: class,
		ConfigurationRevision: snapshot.ConfigurationRevision, EffectiveRuntimeRevision: effectiveRevision,
		PinnedRuntimeIdentity: identity.IdentityRevision, BuildFeature: feature, EffectiveNetworks: networks,
		EffectiveNetworksRevision: snapshot.UDPTransport.EffectiveNetworkRevision, TransportRevision: snapshot.UDPTransport.TransportRevision,
		SocketIntentRevision: snapshot.UDPTransport.SocketIntentRevision, AuthenticationPresent: snapshot.Authentication.Expected,
		AuthenticationCount: snapshot.Authentication.Count, AuthenticationRevision: snapshot.Authentication.Revision,
		TLSPresent: snapshot.TLS.Enabled, TLSSemanticRevision: hostresources.Revision(snapshot.TLS),
		ProtocolOwnedZeroRTT: snapshot.UDPTransport.ProtocolOwnedZeroRTT, ProtocolOwnedMigration: snapshot.UDPTransport.ProtocolOwnedMigration,
		ProtocolOwnedCID: snapshot.UDPTransport.ProtocolOwnedCID, UDPTimeoutRevision: snapshot.UDPTransport.UDPTimeoutRevision,
		ListenerOwnerRevision: snapshot.ConfigurationRevision, RuntimeGenerationRevision: effectiveRevision,
		DependentAssociation: snapshot.UDPTransport.DependentAssociation, ActionableDirectUDPSocket: actionable,
		ObservedAt: observed.Unix(), ExpiresAt: observed.Add(hostresources.MaxInboundTransportFreshnessV2).Unix(), ReasonCodes: reasons,
	}
	return hostresources.FinalizeInboundTransportCapabilityV2(value)
}

func knownTransportClass(value hostresources.InboundTransportClass) bool {
	switch value {
	case hostresources.TransportPlainUDP, hostresources.TransportQUICNative, hostresources.TransportQUICV2Ray,
		hostresources.TransportTCPUDPDual, hostresources.TransportProxyUDPAssociation, hostresources.TransportDNSServiceUnknown,
		hostresources.TransportLocalProxy, hostresources.TransportInterception, hostresources.TransportExternalManaged, hostresources.TransportUnsupported:
		return true
	default:
		return false
	}
}

func containsUDP(values []hostresources.Network) bool {
	for _, value := range values {
		if value == hostresources.NetworkUDP {
			return true
		}
	}
	return false
}
func requiresAuth(value string) bool {
	switch value {
	case "shadowsocks", "naive", "hysteria", "hysteria2", "tuic", "vless", "vmess":
		return true
	}
	return false
}
func requiresTLS(value string, class hostresources.InboundTransportClass) bool {
	return value == "naive" || value == "hysteria" || value == "hysteria2" || value == "tuic" || class == hostresources.TransportQUICV2Ray
}
