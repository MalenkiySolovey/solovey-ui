// Package zapretactivation defines the runtime-only projection returned by
// the authenticated broker. It contains no serialized sing-box options.
package zapretactivation

import (
	"encoding/json"
	"strings"
)

const (
	SocketRoot            = "/run/solovey-ui/zapret"
	MaxBindings           = 3
	CatalogKey            = "zapret.strategies"
	MaxCatalogBytes       = 256 << 10
	MaxDefinitionBytes    = 8 << 10
	ManagedStrategyPrefix = "ui-zapret-"
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
	BatchID  string  `json:"batchId"`
	Bindings []Grant `json:"bindings"`
}

type BatchIdentity struct {
	BatchID string `json:"batchId"`
}

type Retirement = BatchIdentity

type AppliedBinding struct {
	Request
	StrategyRevision string `json:"strategyRevision"`
}

type Validation struct {
	Bindings    []Request                  `json:"bindings"`
	Definitions map[string]json.RawMessage `json:"definitions,omitempty"`
	DNSPorts    map[string][]uint16        `json:"dnsPorts,omitempty"`
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
