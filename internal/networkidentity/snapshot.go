// Package networkidentity owns the host-scoped network fact read contract.
// Facts carry evidence; they never choose Routing, DNS or advertised policy.
package networkidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

const SchemaV1 = "solovey-ui/host-network-identity/v1"

const MaxFacts = 4096

type Availability string

const (
	Available   Availability = "AVAILABLE"
	Unavailable Availability = "UNAVAILABLE"
	Unknown     Availability = "UNKNOWN"
)

type Evidence struct {
	Availability Availability `json:"availability"`
	Source       string       `json:"source"`
	Authority    string       `json:"authority"`
	ObservedAt   int64        `json:"observedAt,omitempty"`
	ExpiresAt    int64        `json:"expiresAt,omitempty"`
	Revision     string       `json:"revision,omitempty"`
	Reason       string       `json:"reason"`
}

func UnknownEvidence(reason string) Evidence {
	if len(reason) == 0 || len(reason) > 96 || strings.ContainsAny(reason, "\r\n\x00") {
		reason = "network_evidence_unavailable"
	}
	return Evidence{Availability: Unknown, Source: "unavailable", Authority: "UNKNOWN", Reason: reason}
}

// At prevents a cached observation from being presented as current truth.
// Old values may remain visible with UNKNOWN evidence for diagnostics only.
func (e Evidence) At(now time.Time) Evidence {
	decoded, err := hex.DecodeString(e.Revision)
	knownSource := e.Source == "host-network/net.Interfaces" || strings.HasPrefix(e.Source, "resource-inventory:")
	knownAuthority := e.Authority == "OS_INTERFACE_OBSERVATION" || e.Authority == "CONFIGURED_INPUT"
	if e.Availability == Available && (e.ObservedAt <= 0 || e.ObservedAt > now.Unix() || e.ExpiresAt <= now.Unix() ||
		e.ExpiresAt > e.ObservedAt+300 || err != nil || len(decoded) != 32 || !knownSource || !knownAuthority) {
		e.Availability, e.Reason = Unknown, "evidence_invalid_or_stale"
	}
	if e.Availability != Available && e.Availability != Unavailable && e.Availability != Unknown {
		e.Availability, e.Reason = Unknown, "evidence_availability_unrecognized"
	}
	return e
}

type Address struct {
	Address  string   `json:"address"`
	Prefix   int      `json:"prefix"`
	Family   string   `json:"family"`
	Scope    string   `json:"scope"`
	Evidence Evidence `json:"evidence"`
}

type Interface struct {
	Index           int       `json:"index"`
	Name            string    `json:"name"`
	MTU             int       `json:"mtu"`
	Up              bool      `json:"up"`
	Loopback        bool      `json:"loopback"`
	VirtualNameHint bool      `json:"virtualNameHint"`
	Role            Evidence  `json:"role"`
	Evidence        Evidence  `json:"evidence"`
	AddressEvidence Evidence  `json:"addressEvidence"`
	Addresses       []Address `json:"addresses"`
}

type AddressRef struct {
	InterfaceIndex int      `json:"interfaceIndex,omitempty"`
	Address        string   `json:"address"`
	Prefix         int      `json:"prefix,omitempty"`
	Destination    string   `json:"destination,omitempty"`
	Evidence       Evidence `json:"evidence"`
}

type AddressFacts struct {
	Evidence Evidence     `json:"evidence"`
	Values   []AddressRef `json:"values"`
}

type EndpointInput struct {
	Kind                  string   `json:"kind"`
	EndpointID            string   `json:"endpointId,omitempty"`
	ResourceID            string   `json:"resourceId"`
	Owner                 string   `json:"owner"`
	Address               string   `json:"address"`
	Port                  uint16   `json:"port"`
	Network               string   `json:"network"`
	ConfigurationRevision string   `json:"configurationRevision"`
	Evidence              Evidence `json:"evidence"`
}

type EndpointFacts struct {
	Evidence Evidence        `json:"evidence"`
	Values   []EndpointInput `json:"values"`
}

type Snapshot struct {
	Schema            string        `json:"schema"`
	Host              hostref.Ref   `json:"host"`
	ObservationScope  string        `json:"observationScope"`
	GeneratedAt       int64         `json:"generatedAt"`
	InterfaceEvidence Evidence      `json:"interfaceEvidence"`
	Interfaces        []Interface   `json:"interfaces"`
	PrivateAddresses  AddressFacts  `json:"privateAddresses"`
	LAN               AddressFacts  `json:"lan"`
	WAN               AddressFacts  `json:"wan"`
	RouteSources      AddressFacts  `json:"routeSources"`
	ExternalEgress    AddressFacts  `json:"externalEgress"`
	Listen            EndpointFacts `json:"listen"`
	Advertised        EndpointFacts `json:"advertised"`
	ForwardedIngress  Evidence      `json:"forwardedIngress"`
	Revision          string        `json:"revision"`
}

func Empty(now time.Time) Snapshot {
	unknownAddresses := func(reason string) AddressFacts {
		return AddressFacts{Evidence: UnknownEvidence(reason), Values: []AddressRef{}}
	}
	unknownEndpoints := func(reason string) EndpointFacts {
		return EndpointFacts{Evidence: UnknownEvidence(reason), Values: []EndpointInput{}}
	}
	return Snapshot{Schema: SchemaV1, Host: hostref.Local(), ObservationScope: "CURRENT_PROCESS_NETWORK_NAMESPACE", GeneratedAt: now.Unix(), Interfaces: []Interface{},
		InterfaceEvidence: UnknownEvidence("interface_provider_unavailable"),
		PrivateAddresses:  unknownAddresses("interface_provider_unavailable"),
		LAN:               unknownAddresses("lan_role_provider_not_shipped"), WAN: unknownAddresses("wan_authority_provider_not_shipped"),
		RouteSources: unknownAddresses("route_source_provider_not_shipped"), ExternalEgress: unknownAddresses("egress_observer_not_shipped"),
		Listen: unknownEndpoints("configured_resource_inventory_unavailable"), Advertised: unknownEndpoints("advertised_inputs_unavailable"),
		ForwardedIngress: UnknownEvidence("INGRESS_SCOPE_AUTHORITY_NOT_SHIPPED")}
}

// Finalize copies the projection and ages every evidence-bearing value. It is
// also the consumer seam for a previously captured snapshot; no stale fact is
// promoted merely because its enclosing snapshot was read again.
func Finalize(value Snapshot, now time.Time) Snapshot {
	if value.Schema != SchemaV1 || !value.Host.IsLocal() || value.ObservationScope != "CURRENT_PROCESS_NETWORK_NAMESPACE" || !boundedSnapshot(value) {
		value = Empty(now)
	}
	value.GeneratedAt, value.Revision = now.Unix(), ""
	value.InterfaceEvidence = value.InterfaceEvidence.At(now)
	value.ForwardedIngress = value.ForwardedIngress.At(now)
	value.Interfaces = append([]Interface{}, value.Interfaces...)
	for index := range value.Interfaces {
		intf := &value.Interfaces[index]
		intf.Evidence, intf.AddressEvidence, intf.Role = intf.Evidence.At(now), intf.AddressEvidence.At(now), intf.Role.At(now)
		intf.Addresses = append([]Address{}, intf.Addresses...)
		for address := range intf.Addresses {
			intf.Addresses[address].Evidence = intf.Addresses[address].Evidence.At(now)
		}
	}
	addresses := []*AddressFacts{&value.PrivateAddresses, &value.LAN, &value.WAN, &value.RouteSources, &value.ExternalEgress}
	for _, facts := range addresses {
		facts.Evidence = facts.Evidence.At(now)
		facts.Values = append([]AddressRef{}, facts.Values...)
		for index := range facts.Values {
			facts.Values[index].Evidence = facts.Values[index].Evidence.At(now)
		}
	}
	for _, facts := range []*EndpointFacts{&value.Listen, &value.Advertised} {
		facts.Evidence = facts.Evidence.At(now)
		facts.Values = append([]EndpointInput{}, facts.Values...)
		for index := range facts.Values {
			facts.Values[index].Evidence = facts.Values[index].Evidence.At(now)
		}
	}
	value.Revision = Revision(value)
	return value
}

func boundedSnapshot(value Snapshot) bool {
	if len(value.Interfaces) > MaxFacts || len(value.Listen.Values) > MaxFacts || len(value.Advertised.Values) > MaxFacts {
		return false
	}
	addresses := 0
	for _, intf := range value.Interfaces {
		addresses += len(intf.Addresses)
		if addresses > MaxFacts {
			return false
		}
	}
	for _, facts := range []AddressFacts{value.PrivateAddresses, value.LAN, value.WAN, value.RouteSources, value.ExternalEgress} {
		if len(facts.Values) > MaxFacts {
			return false
		}
	}
	return true
}

func Revision(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
