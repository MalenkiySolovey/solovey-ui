package registry

import "sync"

const tailscaleBuildTag = "with_tailscale,with_gvisor"

// A declaration keeps implementation eligibility beside its registration.
// Registered option schemas may exist even when the constructor is a stub.
type declaration[R any] struct {
	typeName string
	buildTag string
	platform string
	compiled bool
	register func(R)
}

type Fact struct {
	Category   string `json:"category"`
	Type       string `json:"type"`
	Known      bool   `json:"known"`
	Registered bool   `json:"registered"`
	Compiled   bool   `json:"compiled"`
	BuildTag   string `json:"buildTag,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (f Fact) Available() bool { return f.Known && f.Registered && f.Compiled }

type optionRegistry interface{ CreateOptions(string) (any, bool) }

func factsFor[R optionRegistry](category string, entries []declaration[R], registry R) []Fact {
	facts := make([]Fact, 0, len(entries))
	for _, entry := range entries {
		_, registered := registry.CreateOptions(entry.typeName)
		fact := Fact{Category: category, Type: entry.typeName, Known: true,
			Registered: registered, Compiled: entry.compiled, BuildTag: entry.buildTag, Platform: entry.platform}
		if !fact.Compiled {
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
