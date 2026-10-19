package planner

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const ProtectionResourceRef = "managed-table:" + ProtectionAllocation
const ProtectionEndpointMode = "COEXISTENCE_ENDPOINT_MANAGED"

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

type ProtectionPlan struct {
	Plan
	intent ProtectionEnforcementIntent
}

type Preview struct {
	HostRef         hostref.Ref `json:"hostRef"`
	Owner           string      `json:"owner"`
	ResourceRef     string      `json:"resourceRef"`
	DesiredRevision string      `json:"desiredRevision"`
	InputRevision   string      `json:"inputRevision"`
	PlanRevision    string      `json:"planRevision"`
	Resources       []Ownership `json:"resources"`
}

func (p ProtectionPlan) Preview() Preview {
	intent := p.Intent()
	return Preview{HostRef: intent.HostRef, Owner: intent.Owner, ResourceRef: intent.ResourceRef, DesiredRevision: intent.DesiredRevision,
		InputRevision: intent.InputRevision, PlanRevision: p.Revision(), Resources: p.Resources()}
}

func (p ProtectionPlan) Enforcement() ProtectionEnforcementIntent { return clone(p.intent) }

func CompileProtection(intent ProtectionEnforcementIntent, facts Facts) (ProtectionPlan, error) {
	intent = canonicalProtection(intent)
	if !digest(intent.ArtifactRevision) || intent.Binding.ResourceRef != ProtectionResourceRef ||
		len(intent.Endpoints) > 4096 || len(intent.ManagementExemptions) > 4096 || intent.Mode != "" && intent.Mode != ProtectionEndpointMode {
		return ProtectionPlan{}, conflict(InvalidIntent, intent.Binding.ResourceRef)
	}
	if intent.Mode == ProtectionEndpointMode {
		if intent.Limits.MaxElements < 1 || intent.Limits.MaxElements > 4096 || intent.Limits.DefaultTTLSeconds < 1 || intent.Limits.MaxTTLSeconds > 86400 || intent.Limits.MaxTTLSeconds < intent.Limits.DefaultTTLSeconds ||
			intent.Rates.Gray != (PacketLimit{5, 10}) || intent.Rates.Rate != (PacketLimit{20, 40}) || intent.Rates.Quarantine != (PacketLimit{2, 4}) {
			return ProtectionPlan{}, conflict(InvalidIntent, intent.Binding.ResourceRef)
		}
	}
	names := make(map[string]bool)
	intent.Binding.Resources = nil
	for _, endpoint := range intent.Endpoints {
		binding := endpoint.Binding
		intent.Binding.Resources = append(intent.Binding.Resources, binding)
		if !digest(binding.EndpointRevision) || !validEndpoint(endpoint.Key) || len(endpoint.Contributions) > intent.Limits.MaxElements ||
			endpoint.UDPNew != nil && (*endpoint.UDPNew != (PacketLimit{200, 400}) || endpoint.Key.Network != hostresources.NetworkUDP) {
			return ProtectionPlan{}, conflict(InvalidIntent, binding.ResourceRef)
		}
		name := binding.EndpointRevision[:12]
		if names[name] {
			return ProtectionPlan{}, &Conflict{Reason: AllocationUnavailable, ResourceRef: binding.ResourceRef, Allocation: name}
		}
		names[name] = true
		for _, source := range endpoint.Contributions {
			prefix, err := netip.ParsePrefix(source.Subject)
			if err != nil || prefix.Masked().String() != source.Subject || prefix.Addr().Is4() != (endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv4) ||
				!setClass(source.Class) || source.TTLSeconds < 1 || source.TTLSeconds > intent.Limits.MaxTTLSeconds || source.ExpiresAt <= 0 {
				return ProtectionPlan{}, conflict(InvalidIntent, binding.ResourceRef)
			}
		}
	}
	for _, exemption := range intent.ManagementExemptions {
		prefix, err := netip.ParsePrefix(exemption.SourcePrefix)
		if !validEndpoint(exemption.Key) || err != nil || prefix.Masked().String() != exemption.SourcePrefix || prefix.Addr().Is4() != (exemption.Key.AddressFamily == hostresources.AddressFamilyIPv4) ||
			exemption.RecoveryPathID != "trusted-source" && exemption.ExpiresAt <= 0 {
			return ProtectionPlan{}, conflict(InvalidIntent, intent.Binding.ResourceRef)
		}
	}
	// Bind every materialized decision, including rates and exemptions, to the
	// immutable plan identity in addition to the feature owner's desired revision.
	configuration := clone(intent)
	configuration.Binding = Intent{}
	intent.Binding.ConfigurationRevision = revision(configuration)
	plan, err := Compile(intent.Binding, facts)
	if err != nil {
		return ProtectionPlan{}, err
	}
	add := func(kind, name string) {
		plan.resources = append(plan.resources, Ownership{HostRef: intent.Binding.HostRef, Owner: intent.Binding.Owner, ResourceRef: intent.Binding.ResourceRef,
			PlanRevision: plan.Revision(), Kind: kind, Purpose: intent.Binding.Purpose, Allocation: ProtectionAllocation + ":" + name})
	}
	if intent.Mode == ProtectionEndpointMode {
		add("filter_chain", "chain:solovey_input")
	} else {
		for _, name := range []string{"solovey_input_precheck", "solovey_tcp_public", "solovey_udp_public"} {
			add("filter_chain", "chain:"+name)
		}
		for _, name := range []string{"solovey_allow_tcp_ports", "solovey_allow_udp_ports", "solovey_graylist4", "solovey_graylist6"} {
			add("filter_set", "set:"+name)
		}
	}
	for _, endpoint := range intent.Endpoints {
		ownership := Ownership{HostRef: intent.Binding.HostRef, Owner: intent.Binding.Owner, ResourceRef: endpoint.Binding.ResourceRef,
			PlanRevision: plan.Revision(), Kind: "filter_chain", Purpose: intent.Binding.Purpose, Allocation: ProtectionAllocation + ":chain:solovey_endpoint_" + endpoint.Binding.EndpointRevision[:12]}
		plan.resources = append(plan.resources, ownership)
		classes := make(map[SetClass]bool)
		for _, source := range endpoint.Contributions {
			classes[source.Class] = true
		}
		for _, class := range []SetClass{GraySet, RateSet, QuarantineSet, BlockSet} {
			if classes[class] {
				set := ownership
				family := "4"
				if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv6 {
					family = "6"
				}
				set.Kind, set.Allocation = "filter_set", ProtectionAllocation+":set:solovey_"+string(class)+family+"_"+endpoint.Binding.EndpointRevision[:12]
				plan.resources = append(plan.resources, set)
			}
		}
	}
	return ProtectionPlan{Plan: plan, intent: intent}, nil
}

func canonicalProtection(value ProtectionEnforcementIntent) ProtectionEnforcementIntent {
	value = clone(value)
	sort.Slice(value.Endpoints, func(i, j int) bool {
		return protectionEndpointKey(value.Endpoints[i].Key)+"\x00"+value.Endpoints[i].Binding.ResourceRef < protectionEndpointKey(value.Endpoints[j].Key)+"\x00"+value.Endpoints[j].Binding.ResourceRef
	})
	for index := range value.Endpoints {
		sort.Slice(value.Endpoints[index].Contributions, func(i, j int) bool {
			return revision(value.Endpoints[index].Contributions[i]) < revision(value.Endpoints[index].Contributions[j])
		})
	}
	sort.Slice(value.ManagementExemptions, func(i, j int) bool {
		a, b := value.ManagementExemptions[i], value.ManagementExemptions[j]
		return protectionEndpointKey(a.Key)+"\x00"+a.SourcePrefix+"\x00"+a.RecoveryPathID < protectionEndpointKey(b.Key)+"\x00"+b.SourcePrefix+"\x00"+b.RecoveryPathID
	})
	sort.Ints(value.AllowTCPPorts)
	sort.Ints(value.AllowUDPPorts)
	sort.Strings(value.GraylistCIDRs)
	return value
}

func CanonicalProtectionIntent(value ProtectionEnforcementIntent) ProtectionEnforcementIntent {
	return canonicalProtection(value)
}

func protectionEndpointKey(key hostresources.PublicEndpointKey) string {
	return string(key.Network) + "\x00" + string(key.AddressFamily) + "\x00" + hostresources.NormalizeListen(key.BindAddress).Value + "\x00" + strconv.Itoa(int(key.Port))
}

// ValidProtectionEndpoint checks typed endpoint fields without returning OS syntax.
func ValidProtectionEndpoint(key hostresources.PublicEndpointKey, source string) bool {
	if !validEndpoint(key) {
		return false
	}
	if source == "" {
		return true
	}
	prefix, err := netip.ParsePrefix(source)
	return err == nil && prefix.Masked().String() == source && prefix.Addr().Is4() == (key.AddressFamily == hostresources.AddressFamilyIPv4)
}

func setClass(value SetClass) bool {
	return value == GraySet || value == RateSet || value == QuarantineSet || value == BlockSet
}
func validEndpoint(key hostresources.PublicEndpointKey) bool {
	if key.Port == 0 || key.Network != hostresources.NetworkTCP && key.Network != hostresources.NetworkUDP ||
		key.AddressFamily != hostresources.AddressFamilyIPv4 && key.AddressFamily != hostresources.AddressFamilyIPv6 {
		return false
	}
	listen := hostresources.NormalizeListen(key.BindAddress)
	if listen.Wildcard() {
		return true
	}
	address, err := netip.ParseAddr(listen.Value)
	return err == nil && !strings.Contains(listen.Value, "%") && address.Is4() == (key.AddressFamily == hostresources.AddressFamilyIPv4)
}
