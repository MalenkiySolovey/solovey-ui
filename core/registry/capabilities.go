package registry

import (
	"reflect"
	"slices"
	"sync"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"

	"github.com/MalenkiySolovey/solovey-ui/core/registry/facts"
)

const tailscaleBuildTag = "with_tailscale,with_gvisor"

// A declaration keeps implementation eligibility beside its registration.
// Registered option schemas may exist even when the constructor is a stub.
type declaration[R any] struct {
	typeName           string
	buildTag           string
	platform           string
	compiled           bool
	productUnavailable string
	authenticatedUsers bool
	runtimeDependency  string
	register           func(R)
}

type Fact = facts.Fact

type optionRegistry interface{ CreateOptions(string) (any, bool) }

func factsFor[R optionRegistry](category string, entries []declaration[R], registry R) []Fact {
	facts := make([]Fact, 0, len(entries))
	for _, entry := range entries {
		_, registered := registry.CreateOptions(entry.typeName)
		fact := Fact{Category: category, Type: entry.typeName, Known: true,
			Registered: registered, Compiled: entry.compiled, SupportedByProduct: entry.productUnavailable == "", BuildTag: entry.buildTag, Platform: entry.platform, AuthenticatedUsers: entry.authenticatedUsers, RuntimeDependency: entry.runtimeDependency}
		if !fact.SupportedByProduct {
			fact.Reason = entry.productUnavailable
		} else if !fact.Compiled {
			fact.Reason = "KNOWN_BUT_NOT_COMPILED"
			if fact.Platform != "" {
				fact.Reason = "KNOWN_BUT_UNAVAILABLE_PLATFORM"
			}
		} else if !fact.Registered {
			fact.Reason = "RUNTIME_SCHEMA_UNREGISTERED"
		}
		facts = append(facts, fact)
	}
	return facts
}

var compiledFacts = sync.OnceValue(func() []Fact {
	var result []Fact
	result = append(result, factsFor("inbounds", inboundDeclarations(), InboundRegistry())...)
	result = append(result, factsFor("outbounds", outboundDeclarations(), OutboundRegistry())...)
	result = append(result, factsFor("endpoints", endpointDeclarations(), EndpointRegistry())...)
	result = append(result, factsFor("services", serviceDeclarations(), ServiceRegistry())...)
	result = append(result, factsFor("dns", dnsDeclarations(), DNSTransportRegistry())...)
	result = append(result, factsFor("certificateProviders", certificateDeclarations(), CertificateProviderRegistry())...)
	return result
})

// Facts returns an immutable-by-copy projection of this binary's registrations.
// It says nothing about whether particular option values can start successfully.
func Facts() []Fact { return append([]Fact(nil), compiledFacts()...) }

func Resolve(category, runtimeType string) Fact {
	for _, fact := range compiledFacts() {
		if fact.Category == category && fact.Type == runtimeType {
			return fact
		}
	}
	return Fact{Category: category, Type: runtimeType, Reason: "UNKNOWN_ENTITY_TYPE"}
}

type ProtocolFieldCapability struct {
	Available        bool   `json:"available"`
	RequiredProvider string `json:"requiredProvider,omitempty"`
}

// These optional blocks change endpoint identity/routing. An unavailable block
// makes the preserved resource inactive, rather than stripping it for a provider.
func CriticalProtocolFields() map[string]map[string]ProtocolFieldCapability {
	result := map[string]map[string]ProtocolFieldCapability{}
	for side, typ := range map[string]reflect.Type{"in": reflect.TypeFor[option.Hysteria2InboundOptions](), "out": reflect.TypeFor[option.Hysteria2OutboundOptions]()} {
		result["hysteria2/"+side] = map[string]ProtocolFieldCapability{"realm": {Available: slices.Contains(jsonfields.Names(typ, false), "realm"), RequiredProvider: coreprovider.RequiredProviderName("shtorm_extended")}}
	}
	return result
}
