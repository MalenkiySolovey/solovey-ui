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
	MaxBindings           = 3
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
	OutboundRef      string `json:"outboundRef"`
	OutboundRevision string `json:"outboundRevision"`
	StrategyRef      string `json:"strategyRef"`
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
	StrategyRevision string `json:"strategyRevision"`
	Queue            uint16 `json:"queue,omitempty"`
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

type DNSFamily uint8

const (
	DNSFamilyFromProfile DNSFamily = 0 // Bootstrap can return either supported family.
	DNSFamilyIPv4        DNSFamily = 4
	DNSFamilyIPv6        DNSFamily = 6
)

type DNSPathRequirement struct {
	Port   uint16    `json:"port"`
	Family DNSFamily `json:"family,omitempty"`
	QUIC   bool      `json:"quic,omitempty"` // Otherwise authenticated TLS over TCP.
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
