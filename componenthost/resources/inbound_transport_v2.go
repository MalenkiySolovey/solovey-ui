package resources

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	InboundTransportCapabilitySchemaV2 = "solovey-ui/inbound-transport-capability/v2"
	RuntimeBuildFeatureSchemaV1        = "solovey-ui/runtime-build-feature/v1"
	MaxInboundTransportFactsV2         = 4096
	MaxInboundTransportReasonsV2       = 32
	MaxInboundTransportFreshnessV2     = 5 * time.Minute
)

type InboundTransportClass string

const (
	TransportPlainUDP            InboundTransportClass = "PLAIN_UDP"
	TransportQUICNative          InboundTransportClass = "QUIC_NATIVE"
	TransportQUICV2Ray           InboundTransportClass = "QUIC_V2RAY_TRANSPORT"
	TransportTCPUDPDual          InboundTransportClass = "TCP_UDP_DUAL"
	TransportProxyUDPAssociation InboundTransportClass = "PROXY_UDP_ASSOCIATION"
	TransportDNSServiceUnknown   InboundTransportClass = "DNS_SERVICE_UNKNOWN"
	TransportLocalProxy          InboundTransportClass = "LOCAL_PROXY"
	TransportInterception        InboundTransportClass = "INTERCEPTION"
	TransportExternalManaged     InboundTransportClass = "EXTERNAL_MANAGED"
	TransportUnsupported         InboundTransportClass = "UNSUPPORTED"
)

type RuntimeFeatureState string

const (
	RuntimeFeatureSupported   RuntimeFeatureState = "SUPPORTED"
	RuntimeFeatureUnavailable RuntimeFeatureState = "UNAVAILABLE"
	RuntimeFeatureUnknown     RuntimeFeatureState = "UNKNOWN"
)

type RuntimeBuildFeatureV1 struct {
	Schema               string              `json:"schema"`
	Feature              string              `json:"feature"`
	State                RuntimeFeatureState `json:"state"`
	RuntimeIdentity      string              `json:"runtimeIdentity"`
	SourceRevision       string              `json:"sourceRevision"`
	ModuleRevision       string              `json:"moduleRevision"`
	BuildProfileRevision string              `json:"buildProfileRevision"`
	ObservationMethod    string              `json:"observationMethod"`
	ObservedAt           int64               `json:"observedAt"`
	ExpiresAt            int64               `json:"expiresAt"`
	Revision             string              `json:"revision"`
	ReasonCodes          []string            `json:"reasonCodes,omitempty"`
}

type InboundTransportCapabilityV2 struct {
	Schema                    string                `json:"schema"`
	ProviderID                string                `json:"providerId"`
	ContributorID             string                `json:"contributorId"`
	ResourceID                string                `json:"resourceId"`
	InboundDatabaseID         uint                  `json:"inboundDatabaseId"`
	InboundTag                string                `json:"inboundTag,omitempty"`
	InboundType               string                `json:"inboundType"`
	StrategyClass             InboundTransportClass `json:"strategyClass"`
	ConfigurationRevision     string                `json:"configurationRevision"`
	EffectiveRuntimeRevision  string                `json:"effectiveRuntimeRevision"`
	PinnedRuntimeIdentity     string                `json:"pinnedRuntimeIdentity"`
	BuildFeature              RuntimeBuildFeatureV1 `json:"buildFeature"`
	EffectiveNetworks         []Network             `json:"effectiveNetworks"`
	EffectiveNetworksRevision string                `json:"effectiveNetworksRevision"`
	TransportRevision         string                `json:"transportRevision"`
	SocketIntentRevision      string                `json:"socketIntentRevision"`
	AuthenticationPresent     bool                  `json:"authenticationPresent"`
	AuthenticationCount       int                   `json:"authenticationCount"`
	AuthenticationRevision    string                `json:"authenticationRevision"`
	TLSPresent                bool                  `json:"tlsPresent"`
	TLSSemanticRevision       string                `json:"tlsSemanticRevision"`
	ProtocolOwnedZeroRTT      bool                  `json:"protocolOwnedZeroRtt"`
	ProtocolOwnedMigration    bool                  `json:"protocolOwnedMigration"`
	ProtocolOwnedCID          bool                  `json:"protocolOwnedCid"`
	UDPTimeoutRevision        string                `json:"udpTimeoutRevision"`
	ListenerOwnerRevision     string                `json:"listenerOwnerRevision"`
	RuntimeGenerationRevision string                `json:"runtimeGenerationRevision"`
	DependentAssociation      bool                  `json:"dependentAssociation"`
	ActionableDirectUDPSocket bool                  `json:"actionableDirectUdpSocket"`
	ObservedAt                int64                 `json:"observedAt"`
	ExpiresAt                 int64                 `json:"expiresAt"`
	Revision                  string                `json:"revision"`
	ReasonCodes               []string              `json:"reasonCodes,omitempty"`
}

func (f RuntimeBuildFeatureV1) Validate(now time.Time) error {
	if f.Schema != RuntimeBuildFeatureSchemaV1 || f.Feature != "with_quic" ||
		(f.State != RuntimeFeatureSupported && f.State != RuntimeFeatureUnavailable && f.State != RuntimeFeatureUnknown) ||
		!boundedToken(f.RuntimeIdentity, 128) || !boundedToken(f.SourceRevision, 128) ||
		!boundedToken(f.ModuleRevision, 128) || !boundedToken(f.BuildProfileRevision, 128) ||
		!boundedToken(f.ObservationMethod, 64) || f.ObservedAt <= 0 || f.ExpiresAt <= f.ObservedAt ||
		f.ExpiresAt-f.ObservedAt > int64(MaxInboundTransportFreshnessV2/time.Second) ||
		(!now.IsZero() && f.ExpiresAt <= now.UTC().Unix()) || !digestToken(f.Revision) ||
		f.Revision != Revision(runtimeFeatureRevisionInput(f)) || !validTransportReasons(f.ReasonCodes) {
		return errors.New("runtime_build_feature_v1_invalid")
	}
	return nil
}

func (f InboundTransportCapabilityV2) Validate(now time.Time) error {
	if f.Schema != InboundTransportCapabilitySchemaV2 || !boundedToken(f.ProviderID, 128) ||
		!boundedToken(f.ContributorID, 128) || !boundedToken(f.ResourceID, 256) || f.InboundDatabaseID == 0 ||
		!boundedToken(f.InboundType, 64) || !validTransportClass(f.StrategyClass) ||
		!digestToken(f.ConfigurationRevision) || !digestToken(f.EffectiveRuntimeRevision) ||
		!boundedToken(f.PinnedRuntimeIdentity, 128) || f.AuthenticationCount < 0 || f.AuthenticationCount > 65535 ||
		!digestToken(f.EffectiveNetworksRevision) || !digestToken(f.TransportRevision) ||
		!digestToken(f.SocketIntentRevision) || !digestToken(f.AuthenticationRevision) ||
		!digestToken(f.TLSSemanticRevision) || !digestToken(f.UDPTimeoutRevision) ||
		!boundedToken(f.ListenerOwnerRevision, 128) || !boundedToken(f.RuntimeGenerationRevision, 128) ||
		f.ObservedAt <= 0 || f.ExpiresAt <= f.ObservedAt || f.ExpiresAt-f.ObservedAt > int64(MaxInboundTransportFreshnessV2/time.Second) ||
		(!now.IsZero() && f.ExpiresAt <= now.UTC().Unix()) || !digestToken(f.Revision) ||
		!validEffectiveNetworks(f.EffectiveNetworks) || !validTransportReasons(f.ReasonCodes) ||
		f.Revision != Revision(inboundTransportRevisionInput(f)) {
		return errors.New("inbound_transport_capability_v2_invalid")
	}
	if err := f.BuildFeature.Validate(now); err != nil {
		return err
	}
	if f.AuthenticationPresent != (f.AuthenticationCount > 0) || f.DependentAssociation && f.ActionableDirectUDPSocket ||
		f.ActionableDirectUDPSocket && !containsNetwork(f.EffectiveNetworks, NetworkUDP) ||
		(f.BuildFeature.State != RuntimeFeatureSupported && (f.StrategyClass == TransportQUICNative || f.StrategyClass == TransportQUICV2Ray) && f.ActionableDirectUDPSocket) {
		return errors.New("inbound_transport_capability_v2_contradictory")
	}
	return nil
}

func FinalizeRuntimeBuildFeatureV1(value RuntimeBuildFeatureV1) RuntimeBuildFeatureV1 {
	value.Schema = RuntimeBuildFeatureSchemaV1
	value.ReasonCodes = canonicalTransportReasons(value.ReasonCodes)
	value.Revision = Revision(runtimeFeatureRevisionInput(value))
	return value
}

func FinalizeInboundTransportCapabilityV2(value InboundTransportCapabilityV2) InboundTransportCapabilityV2 {
	value.Schema = InboundTransportCapabilitySchemaV2
	value.EffectiveNetworks = canonicalNetworks(value.EffectiveNetworks)
	value.ReasonCodes = canonicalTransportReasons(value.ReasonCodes)
	value.Revision = Revision(inboundTransportRevisionInput(value))
	return value
}

func runtimeFeatureRevisionInput(value RuntimeBuildFeatureV1) RuntimeBuildFeatureV1 {
	value.ObservedAt, value.ExpiresAt, value.Revision = 0, 0, ""
	return value
}

func inboundTransportRevisionInput(value InboundTransportCapabilityV2) InboundTransportCapabilityV2 {
	value.ObservedAt, value.ExpiresAt, value.Revision = 0, 0, ""
	value.BuildFeature.ObservedAt, value.BuildFeature.ExpiresAt = 0, 0
	return value
}

func validTransportClass(value InboundTransportClass) bool {
	switch value {
	case TransportPlainUDP, TransportQUICNative, TransportQUICV2Ray, TransportTCPUDPDual,
		TransportProxyUDPAssociation, TransportDNSServiceUnknown, TransportLocalProxy,
		TransportInterception, TransportExternalManaged, TransportUnsupported:
		return true
	default:
		return false
	}
}

func validEffectiveNetworks(values []Network) bool {
	if len(values) == 0 || len(values) > 2 {
		return false
	}
	canonical := canonicalNetworks(values)
	if len(canonical) != len(values) {
		return false
	}
	for index := range values {
		if values[index] != canonical[index] || (values[index] != NetworkTCP && values[index] != NetworkUDP) {
			return false
		}
	}
	return true
}

func canonicalNetworks(values []Network) []Network {
	seen := map[Network]bool{}
	result := make([]Network, 0, len(values))
	for _, value := range values {
		if (value == NetworkTCP || value == NetworkUDP) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func containsNetwork(values []Network, target Network) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func canonicalTransportReasons(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if boundedToken(value, 96) && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	if len(result) > MaxInboundTransportReasonsV2 {
		result = result[:MaxInboundTransportReasonsV2]
	}
	return result
}

func validTransportReasons(values []string) bool {
	canonical := canonicalTransportReasons(values)
	if len(canonical) != len(values) {
		return false
	}
	for index := range values {
		if values[index] != canonical[index] {
			return false
		}
	}
	return true
}

func boundedToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "\r\n\t{}[]<>\"'\\") {
		return false
	}
	return true
}

func digestToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
