package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"
)

const EndpointSchemaV1 = "solovey-ui/public-endpoint/v1"

type Network string

const (
	NetworkTCP     Network = "tcp"
	NetworkUDP     Network = "udp"
	NetworkUnknown Network = "unknown"
)

type AddressFamily string

const (
	AddressFamilyIPv4    AddressFamily = "ipv4"
	AddressFamilyIPv6    AddressFamily = "ipv6"
	AddressFamilyUnknown AddressFamily = "unknown"
)

type EndpointIntent string

const (
	EndpointIntentPublic  EndpointIntent = "public"
	EndpointIntentPrivate EndpointIntent = "private"
	EndpointIntentLocal   EndpointIntent = "local"
	EndpointIntentUnknown EndpointIntent = "unknown"
)

// PublicEndpointKey is the exact socket identity used by inventory and
// collision planning. Unknown facts intentionally use unknown family/network
// and never masquerade as a usable key.
type PublicEndpointKey struct {
	Network       Network       `json:"network"`
	AddressFamily AddressFamily `json:"addressFamily"`
	BindAddress   string        `json:"bindAddress"`
	Port          uint16        `json:"port"`
}

type AdvertisedEndpoint struct {
	ID             string   `json:"id"`
	HostnameOrIP   string   `json:"hostnameOrIp"`
	Port           uint16   `json:"port"`
	Network        Network  `json:"network"`
	ProtocolLabel  string   `json:"protocolLabel"`
	RouteSelectors []string `json:"routeSelectors,omitempty"`
	SocketClaimIDs []string `json:"socketClaimIds,omitempty"`
}

type PublicEndpoint struct {
	Schema                 string            `json:"schema"`
	ID                     string            `json:"id"`
	Key                    PublicEndpointKey `json:"key"`
	Intent                 EndpointIntent    `json:"intent"`
	Protocol               string            `json:"protocol"`
	Transport              string            `json:"transport,omitempty"`
	TLS                    CapabilityValue   `json:"tls"`
	Reality                CapabilityValue   `json:"reality"`
	AuthenticationExpected CapabilityValue   `json:"authenticationExpected"`
	FallbackSupported      CapabilityValue   `json:"fallbackSupported"`
	ProxyProtocol          CapabilityValue   `json:"proxyProtocol"`
	ResourceID             string            `json:"resourceId"`
	Owner                  string            `json:"owner"`
	OwnerRevision          string            `json:"ownerRevision"`
	ConfigurationRevision  string            `json:"configurationRevision"`
	ObservedAt             int64             `json:"observedAt"`
	Source                 string            `json:"source"`
	ConfidenceBP           int               `json:"confidenceBp"`
	ReasonCodes            []string          `json:"reasonCodes,omitempty"`
}

func (e PublicEndpoint) Known() bool {
	return e.Key.Network != NetworkUnknown && e.Key.AddressFamily != AddressFamilyUnknown && e.Key.Port != 0 && len(e.ReasonCodes) == 0
}

func EndpointIntentForBind(value string) EndpointIntent {
	normalized := NormalizeListen(value)
	if normalized.Class == ListenLoopback {
		return EndpointIntentLocal
	}
	if addr, err := netip.ParseAddr(normalized.Value); err == nil && addr.IsPrivate() {
		return EndpointIntentPrivate
	}
	if normalized.Class == ListenHostname || normalized.Class == ListenWildcard {
		return EndpointIntentUnknown
	}
	return EndpointIntentPublic
}

func AddressFamilyForListen(value string) AddressFamily {
	switch NormalizeListen(value).Family {
	case 4:
		return AddressFamilyIPv4
	case 6:
		return AddressFamilyIPv6
	default:
		return AddressFamilyUnknown
	}
}

func NetworkForProtocol(value string) Network {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "udp", "quic":
		return NetworkUDP
	case "tcp", "stream", "http", "https":
		return NetworkTCP
	default:
		return NetworkUnknown
	}
}

func BuildEndpointFact(resource ProtectableResource, network Network, now time.Time, reasons ...string) PublicEndpoint {
	normalized := NormalizeListen(resource.Listen)
	family := AddressFamilyForListen(resource.Listen)
	port := uint16(0)
	if resource.Port > 0 && resource.Port <= 65535 {
		port = uint16(resource.Port)
	} else {
		reasons = append(reasons, "endpoint_port_unknown")
	}
	if network == NetworkUnknown {
		reasons = append(reasons, "endpoint_network_unknown")
	}
	if family == AddressFamilyUnknown {
		reasons = append(reasons, "endpoint_address_family_unknown")
	}
	reasons = normalizedReasonCodes(reasons)
	key := PublicEndpointKey{Network: network, AddressFamily: family, BindAddress: normalized.Value, Port: port}
	id := endpointFactID(resource.ID, key)
	return PublicEndpoint{
		Schema: EndpointSchemaV1, ID: id, Key: key, Intent: EndpointIntentForBind(resource.Listen),
		Protocol: resource.Protocol, TLS: capabilityFromBool(resource.TLS), Reality: CapabilityUnknown,
		AuthenticationExpected: CapabilityUnknown, FallbackSupported: resource.Capabilities.CanServeFallback,
		ProxyProtocol: resource.Capabilities.AcceptsProxyProtocol, ResourceID: resource.ID, Owner: resource.Owner,
		OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision,
		ObservedAt: now.UTC().Unix(), Source: strings.TrimSpace(resource.Source), ConfidenceBP: endpointConfidence(reasons),
		ReasonCodes: reasons,
	}
}

func normalizeEndpointFact(resource ProtectableResource, value PublicEndpoint, now time.Time) PublicEndpoint {
	reasons := append([]string(nil), value.ReasonCodes...)
	value.Schema = EndpointSchemaV1
	switch value.Key.Network {
	case NetworkTCP, NetworkUDP:
	default:
		value.Key.Network = NetworkUnknown
		reasons = append(reasons, "endpoint_network_unknown")
	}
	if strings.ContainsAny(value.Key.BindAddress, "/\\?#&={}[]<>\"'\r\n\t ") {
		value.Key.BindAddress = "*"
		value.Key.AddressFamily = AddressFamilyUnknown
		reasons = append(reasons, "endpoint_bind_invalid")
	} else {
		value.Key.BindAddress = NormalizeListen(value.Key.BindAddress).Value
		actualFamily := AddressFamilyForListen(value.Key.BindAddress)
		if value.Key.AddressFamily != actualFamily {
			value.Key.AddressFamily = actualFamily
			reasons = append(reasons, "endpoint_address_family_mismatch")
		}
	}
	if value.Key.AddressFamily != AddressFamilyIPv4 && value.Key.AddressFamily != AddressFamilyIPv6 {
		value.Key.AddressFamily = AddressFamilyUnknown
		reasons = append(reasons, "endpoint_address_family_unknown")
	}
	if value.Key.Port == 0 || (resource.Port > 0 && resource.Port <= 65535 && value.Key.Port != uint16(resource.Port)) {
		reasons = append(reasons, "endpoint_port_unknown")
	}
	if value.ObservedAt <= 0 || value.ObservedAt > now.UTC().Add(5*time.Minute).Unix() {
		value.ObservedAt = now.UTC().Unix()
		reasons = append(reasons, "endpoint_observation_time_invalid")
	}
	value.Intent = EndpointIntentForBind(value.Key.BindAddress)
	value.Protocol = safeEndpointToken(value.Protocol)
	value.Transport = safeEndpointToken(value.Transport)
	value.TLS = normalizeCapability(value.TLS)
	value.Reality = normalizeCapability(value.Reality)
	value.AuthenticationExpected = normalizeCapability(value.AuthenticationExpected)
	value.FallbackSupported = normalizeCapability(value.FallbackSupported)
	value.ProxyProtocol = normalizeCapability(value.ProxyProtocol)
	value.ResourceID = resource.ID
	value.Owner = resource.Owner
	value.OwnerRevision = resource.Capabilities.OwnerRevision
	value.ConfigurationRevision = resource.Capabilities.ConfigRevision
	value.Source = safeEndpointToken(resource.Source)
	if value.Source == "" {
		value.Source = "unknown"
		reasons = append(reasons, "endpoint_source_unknown")
	}
	value.ReasonCodes = normalizedReasonCodes(reasons)
	value.ConfidenceBP = endpointConfidence(value.ReasonCodes)
	value.ID = endpointFactID(resource.ID, value.Key)
	return value
}

func safeEndpointToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return ""
	}
	return value
}

func endpointFactID(resourceID string, key PublicEndpointKey) string {
	payload, _ := json.Marshal(struct {
		ResourceID string            `json:"resource_id"`
		Key        PublicEndpointKey `json:"key"`
	}{strings.TrimSpace(resourceID), key})
	sum := sha256.Sum256(payload)
	return "endpoint:" + hex.EncodeToString(sum[:16])
}

func capabilityFromBool(value bool) CapabilityValue {
	if value {
		return CapabilityYes
	}
	return CapabilityNo
}

func endpointConfidence(reasons []string) int {
	if len(reasons) == 0 {
		return 10000
	}
	return 0
}

func normalizedReasonCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if safeEndpointToken(value) == "" {
			value = "endpoint_reason_invalid"
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 32 {
			break
		}
	}
	sort.Strings(result)
	return result
}
