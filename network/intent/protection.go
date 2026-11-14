// Package intent contains typed host-network requests. It provides no resource
// allocation, acceptance receipt, execution, registry or policy evaluator.
package intent

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

// The initial allocator owns the existing coexistence table only. There is no
// arbitrary table, mark, queue, route, command or global host reset operation.
const ProtectionAllocation = "inet:solovey_protection"
const ProtectionFamily = "inet"
const ProtectionTable = "solovey_protection"
const ProtectionTableCLI = ProtectionFamily + " " + ProtectionTable

const ProtectionResourceRef = "managed-table:" + ProtectionAllocation
const ProtectionEndpointMode = "COEXISTENCE_ENDPOINT_MANAGED"

// ResourceBinding references a canonical owner resource. EndpointRevision is
// the owner's exact effective socket identity, never a primary copied port ID.
type ResourceBinding struct {
	ResourceRef      string `json:"resourceRef"`
	Owner            string `json:"owner"`
	DesiredRevision  string `json:"desiredRevision"`
	EndpointRevision string `json:"endpointRevision"`
	Management       bool   `json:"management"`
}

type Intent struct {
	HostRef               hostref.Ref       `json:"hostRef"`
	Owner                 string            `json:"owner"`
	ResourceRef           string            `json:"resourceRef"`
	DesiredRevision       string            `json:"desiredRevision"`
	ConfigurationRevision string            `json:"configurationRevision,omitempty"`
	InputRevision         string            `json:"inputRevision"`
	Purpose               string            `json:"purpose"`
	Resources             []ResourceBinding `json:"resources"`
}

type SetClass string

const (
	GraySet       SetClass = "gray"
	RateSet       SetClass = "rate"
	QuarantineSet SetClass = "quarantine"
	BlockSet      SetClass = "block"
)

type TimedSource struct {
	Subject    string   `json:"subject"`
	Class      SetClass `json:"class"`
	ExpiresAt  int64    `json:"expiresAt"`
	TTLSeconds int      `json:"ttlSeconds"`
}

// Limits and rates are explicit owner decisions. Planner does not classify
// traffic, choose a policy, or create another feature desired-state store.
type ProtectionLimits struct {
	MaxElements       int `json:"maxElements"`
	DefaultTTLSeconds int `json:"defaultTtlSeconds"`
	MaxTTLSeconds     int `json:"maxTtlSeconds"`
}

type PacketLimit struct {
	Rate  int `json:"rate"`
	Burst int `json:"burst"`
}

type ProtectionRates struct {
	Gray, Rate, Quarantine PacketLimit
}

type ProtectionEndpoint struct {
	Binding       ResourceBinding                 `json:"binding"`
	Key           hostresources.PublicEndpointKey `json:"key"`
	Contributions []TimedSource                   `json:"contributions"`
	UDPNew        *PacketLimit                    `json:"udpNew,omitempty"`
}

type ProtectionExemption struct {
	Key            hostresources.PublicEndpointKey `json:"key"`
	SourcePrefix   string                          `json:"sourcePrefix"`
	ExpiresAt      int64                           `json:"expiresAt"`
	RecoveryPathID string                          `json:"recoveryPathId"`
}

// ProtectionEnforcementIntent contains only the already existing enforcement
// vocabulary. Resource bindings are semantic refs resolved by the owner.
type ProtectionEnforcementIntent struct {
	Binding              Intent                `json:"binding"`
	ArtifactRevision     string                `json:"artifactRevision"`
	Mode                 string                `json:"mode"`
	Endpoints            []ProtectionEndpoint  `json:"endpoints"`
	ManagementExemptions []ProtectionExemption `json:"managementExemptions"`
	Limits               ProtectionLimits      `json:"limits"`
	Rates                ProtectionRates       `json:"rates"`
	AllowTCPPorts        []int                 `json:"allowTcpPorts"`
	AllowUDPPorts        []int                 `json:"allowUdpPorts"`
	GraylistCIDRs        []string              `json:"graylistCidrs"`
}
