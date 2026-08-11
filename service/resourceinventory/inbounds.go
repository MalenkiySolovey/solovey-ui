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

type inboundContributor struct {
	db      *gorm.DB
	control *coreinboundcontrol.Service
}

func (inboundContributor) Owner() string { return "core" }

func (c inboundContributor) ListProtectableResources(ctx context.Context) ([]hostresources.ProtectableResource, error) {
	if c.db == nil {
		return nil, fmt.Errorf("inbound database is unavailable")
	}
	if c.control == nil {
		return nil, fmt.Errorf("inbound control is unavailable")
	}
	snapshots, err := c.control.ListSnapshots(ctx, hostresources.MaxResourceFacts+1)
	if err != nil {
		return nil, err
	}
	result := make([]hostresources.ProtectableResource, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, inboundResource(snapshot))
	}
	return result, nil
}

func inboundResource(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) hostresources.ProtectableResource {
	return inboundResourceAt(snapshot, time.Now())
}

func inboundResourceAt(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, observedAt time.Time) hostresources.ProtectableResource {
	normalized := hostresources.NormalizeListen(snapshot.Listener.Bind)
	warnings := make([]string, 0, len(snapshot.ReasonCodes)+len(snapshot.Effective.ReasonCodes))
	for _, reason := range snapshot.ReasonCodes {
		warnings = append(warnings, string(reason))
	}
	for _, reason := range snapshot.Effective.ReasonCodes {
		warnings = append(warnings, string(reason))
	}
	warnings = uniqueStrings(warnings)
	canFallback := capabilityValue(snapshot.Capability.Disposition)
	protocol := protocolForNetwork(snapshot.Listener.Network, snapshot.Type)
	resource := hostresources.ProtectableResource{
		ID:         snapshot.ResourceID,
		Kind:       "inbound",
		Owner:      "core",
		Name:       snapshot.Tag,
		Protocol:   protocol,
		Listen:     normalized.Value,
		Port:       int(snapshot.Listener.Port),
		Public:     normalized.Public(),
		TLS:        snapshot.TLS.Enabled,
		Source:     "inbounds",
		InboundTag: snapshot.Tag,
		Capabilities: hostresources.ProtectableResourceCapabilities{
			Known:                 configurationKnown(snapshot),
			AcceptsProxyProtocol:  inboundProxyProtocol(snapshot),
			SupportsGracefulDrain: hostresources.CapabilityUnknown,
			CanServeFallback:      canFallback,
			RequiresACMEHTTP01:    hostresources.CapabilityUnknown,
			RequiresTLSALPN01:     hostresources.CapabilityUnknown,
			TLSMode:               tlsMode(snapshot.TLS.Enabled),
			OwnerRevision:         snapshot.ConfigurationRevision,
			ConfigRevision:        snapshot.ConfigurationRevision,
			ExpectedListenerOwner: expectedApplicationListenerOwner(),
		},
		Warnings: warnings,
	}
	networks := endpointNetworks(snapshot.Listener.Network)
	resource.ListenIntents = make([]hostresources.ConfiguredListenIntentV1, 0, len(networks))
	for _, network := range networks {
		if network == hostresources.NetworkTCP || network == hostresources.NetworkUDP {
			intent := hostresources.BuildConfiguredListenIntent(resource)
			intent.Network = network
			resource.ListenIntents = append(resource.ListenIntents, intent)
		}
	}
	resource.Endpoints = make([]hostresources.PublicEndpoint, 0, len(networks))
	for _, network := range networks {
		reasons := []string(nil)
		if network == hostresources.NetworkUnknown {
			reasons = append(reasons, "inbound_network_unknown")
		}
		endpoint := hostresources.BuildEndpointFact(resource, network, observedAt, reasons...)
		endpoint.Transport = snapshot.Transport.Type
		endpoint.Reality = capabilityBool(snapshot.TLS.Reality.Enabled, snapshot.TLS.Reality.Present)
		endpoint.AuthenticationExpected = inboundAuthentication(snapshot)
		endpoint.FallbackSupported = canFallback
		resource.Endpoints = append(resource.Endpoints, endpoint)
	}
	return resource
}

func capabilityValue(disposition coreinboundcontrol.CapabilityDisposition) hostresources.CapabilityValue {
	switch disposition {
	case coreinboundcontrol.CapabilitySupported, coreinboundcontrol.CapabilitySupportedNaturalFallback:
		return hostresources.CapabilityYes
	case coreinboundcontrol.CapabilityUnsupported, coreinboundcontrol.CapabilityNotShipped, coreinboundcontrol.CapabilityOutOfScope:
		return hostresources.CapabilityNo
	default:
		return hostresources.CapabilityUnknown
	}
}

func configurationKnown(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) bool {
	if snapshot.ConfigurationRevision == "" || snapshot.Listener.Network == "unknown" || snapshot.Listener.Port == 0 {
		return false
	}
	for _, reason := range snapshot.ReasonCodes {
		switch reason {
		case coreinboundcontrol.ReasonInboundOptionsMalformed, coreinboundcontrol.ReasonTLSReferenceMissing,
			coreinboundcontrol.ReasonTLSReferenceMismatch, coreinboundcontrol.ReasonTLSOptionsMalformed,
			coreinboundcontrol.ReasonInboundShapeUnknown:
			return false
		}
	}
	return true
}

func endpointNetworks(network string) []hostresources.Network {
	switch network {
	case "tcp":
		return []hostresources.Network{hostresources.NetworkTCP}
	case "udp":
		return []hostresources.Network{hostresources.NetworkUDP}
	case "tcp_udp":
		return []hostresources.Network{hostresources.NetworkTCP, hostresources.NetworkUDP}
	default:
		return []hostresources.Network{hostresources.NetworkUnknown}
	}
}

func protocolForNetwork(network, inboundType string) string {
	switch network {
	case "udp":
		return "udp"
	case "tcp", "tcp_udp":
		if strings.EqualFold(strings.TrimSpace(inboundType), "http") {
			return "http"
		}
		return "stream"
	default:
		return "unknown"
	}
}

func capabilityBool(value, known bool) hostresources.CapabilityValue {
	if !known {
		return hostresources.CapabilityUnknown
	}
	if value {
		return hostresources.CapabilityYes
	}
	return hostresources.CapabilityNo
}

func inboundProxyProtocol(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) hostresources.CapabilityValue {
	if snapshot.Listener.ProxyProtocol {
		return hostresources.CapabilityYes
	}
	return hostresources.CapabilityUnknown
}

func inboundAuthentication(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) hostresources.CapabilityValue {
	if snapshot.Authentication.Known {
		return capabilityBool(snapshot.Authentication.Expected, true)
	}
	switch strings.ToLower(strings.TrimSpace(snapshot.Type)) {
	case "shadowsocks", "trojan", "vless", "vmess", "anytls", "hysteria", "hysteria2", "tuic", "naive", "shadowtls":
		return hostresources.CapabilityYes
	default:
		return hostresources.CapabilityUnknown
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
