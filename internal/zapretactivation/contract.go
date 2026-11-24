// Package zapretactivation defines the runtime-only projection returned by
// the authenticated broker. It contains no serialized sing-box options.
package zapretactivation

import (
	"encoding/json"
	"net/netip"
	"strings"
)

const (
	SocketRoot            = "/run/solovey-ui/zapret"
	CatalogKey            = "zapret.strategies"
	MaxCatalogBytes       = 256 << 10
	MaxDefinitionBytes    = 8 << 10
	ManagedStrategyPrefix = "ui-zapret-"
	TUNInterface          = "svzap-tun0"
)

// Keep operator-owned profile IDs and editable IDs in separate namespaces.
func ManagedStrategyRef(ref string) bool {
	return strings.HasPrefix(ref, ManagedStrategyPrefix)
}

type Request struct {
	OutboundRef      string      `json:"outboundRef"`
	OutboundRevision string      `json:"outboundRevision"`
	StrategyRef      string      `json:"strategyRef"`
	Dial             DialBinding `json:"dial,omitzero"`
	Outer            OuterSocket `json:"outer,omitzero"`
}

// OuterSocket identifies the proxy transport, not its inner destinations.
// The zero value represents the existing destination-dependent Direct path.
type OuterSocket struct {
	Kind   string   `json:"kind"`
	Port   uint16   `json:"port"`
	Family IPFamily `json:"family,omitempty"`
}

func (s OuterSocket) Valid() bool {
	switch s.Kind {
	case "":
		return s == (OuterSocket{})
	case "http", "hysteria2":
		return s.Port != 0 && (s.Family == FamilyFromProfile || s.Family == FamilyIPv4 || s.Family == FamilyIPv6)
	default:
		return false
	}
}

// DialBinding records the requested native settings, not a network grant.
// The broker resolves the interface against its own installed profile.
type DialBinding struct {
	Interface string     `json:"interface,omitempty"`
	IPv4      netip.Addr `json:"ipv4,omitzero"`
	IPv6      netip.Addr `json:"ipv6,omitzero"`
}

func (d DialBinding) Valid() bool {
	return len(d.Interface) <= 15 && !strings.ContainsAny(d.Interface, "\x00/ \t\r\n") &&
		(!d.IPv4.IsValid() || d.IPv4.Is4() && d.IPv4.IsGlobalUnicast()) &&
		(!d.IPv6.IsValid() || d.IPv6.Is6() && !d.IPv6.Is4In6() && d.IPv6.Zone() == "" && d.IPv6.IsGlobalUnicast())
}

type Grant struct {
	Request
	StrategyRevision string `json:"strategyRevision"`
	Generation       string `json:"generation"`
}

// BatchID lets the Panel retire an ambiguous acquire (lost response) without
// retiring a different generation. A batch is private to one Core startup.
type BatchRequest struct {
	BatchID string `json:"batchId"`
	Validation
}

type BatchGrant struct {
	BatchID  string      `json:"batchId"`
	Bindings []Grant     `json:"bindings"`
	TUN      *TUNRequest `json:"tun,omitempty"`
}

type BatchIdentity struct {
	BatchID string `json:"batchId"`
}

type Retirement = BatchIdentity

type AppliedBinding struct {
	Request
	StrategyRevision string         `json:"strategyRevision"`
	Queue            uint16         `json:"queue,omitempty"`
	NativeMatches    *NativeMatches `json:"nativeMatches,omitempty"`
}

// NativeMatches is scoped to an applied binding and its current engine, and
// says nothing about a packet verdict or the result of a native action.
type NativeMatches struct {
	Profiles []string `json:"profiles,omitempty"`
}

type Validation struct {
	Bindings    []Request                       `json:"bindings"`
	Definitions map[string]json.RawMessage      `json:"definitions,omitempty"`
	DNS         map[string][]DNSPathRequirement `json:"dns,omitempty"`
	TUN         *TUNRequest                     `json:"tun,omitempty"`
}

// TUNRequest identifies one candidate inbound; only the installed broker
// profile can authorize the privileged device and network route scope.
type TUNRequest struct {
	Tag     string       `json:"tag"`
	Address netip.Prefix `json:"address"`
	MTU     uint32       `json:"mtu"`
}

func (r TUNRequest) Valid() bool {
	return r.Tag != "" && len(r.Tag) <= 64 && r.Address.Addr().Is4() &&
		r.Address.Addr().IsGlobalUnicast() && r.Address.Bits() >= 24 && r.MTU >= 1280 && r.MTU <= 1500
}

func TUNSocketPath(batch string) (string, bool) {
	if !ValidDigest(batch) {
		return "", false
	}
	return SocketRoot + "/t-" + batch, true
}

type IPFamily uint8

const (
	FamilyFromProfile IPFamily = 0 // A hostname can use either admitted family.
	FamilyIPv4        IPFamily = 4
	FamilyIPv6        IPFamily = 6

	DNSFamilyFromProfile = FamilyFromProfile
	DNSFamilyIPv4        = FamilyIPv4
	DNSFamilyIPv6        = FamilyIPv6
)

type DNSFamily = IPFamily

type DNSPathRequirement struct {
	Port     uint16    `json:"port"`
	Family   DNSFamily `json:"family,omitempty"`
	QUIC     bool      `json:"quic,omitempty"`
	PlainUDP bool      `json:"plainUDP,omitempty"` // UDP with same-detour TCP retry on truncation.
}

func (g Grant) ProtectPath(want Request) (string, bool) {
	if g.Request != want || !ValidDigest(want.OutboundRevision) || !ValidDigest(g.StrategyRevision) || !ValidDigest(g.Generation) {
		return "", false
	}
	return SocketRoot + "/z-" + g.Generation, true
}

func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
