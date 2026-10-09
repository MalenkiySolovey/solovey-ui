// Package capabilities projects entity-owned identities over official build
// facts. It does not own persistence, option validation or runtime application.
package capabilities

import (
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
)

const Schema = "solovey-ui/entity-capabilities/v1"

type Fact struct {
	registry.Fact
	RuntimeType                 string `json:"runtimeType"`
	ContextSupported            bool   `json:"contextSupported"`
	Available                   bool   `json:"available"`
	RuntimeEligible             *bool  `json:"runtimeEligible,omitempty"`
	PlatformDependencyAvailable *bool  `json:"platformDependencyAvailable,omitempty"`
}

type Snapshot struct {
	Schema              string   `json:"schema"`
	ComponentProfile    string   `json:"componentProfile"`
	Facts               []Fact   `json:"facts"`
	MulticastInterfaces []string `json:"multicastInterfaces,omitempty"`
}

func Resolve(category, panelType string) Fact {
	if registry.Resolve(category, entitytypes.RuntimeType(category, panelType)).RuntimeDependency != "" {
		return ResolveWithEnvironment(category, panelType, ObserveEnvironment())
	}
	return ResolveWithEnvironment(category, panelType, Environment{})
}

func ResolveWithEnvironment(category, panelType string, environment Environment) Fact {
	runtimeType := entitytypes.RuntimeType(category, panelType)
	runtimeFact := registry.Resolve(category, runtimeType)
	contextSupported := runtimeFact.Known
	if !contextSupported {
		for _, fact := range registry.Facts() {
			if fact.Type == panelType {
				runtimeFact.Known = true
				break
			}
		}
		for _, mapping := range entitytypes.Mappings() {
			if mapping.Type == panelType {
				runtimeFact.Known = true
				break
			}
		}
		if runtimeFact.Known {
			runtimeFact.Reason = "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT"
		}
	}
	runtimeFact.Type = panelType
	eligible, dependencyAvailable, reason := environmentEligibility(runtimeFact.RuntimeDependency, environment)
	if contextSupported && runtimeFact.Available() && reason != "" {
		runtimeFact.Reason = reason
	}
	return Fact{Fact: runtimeFact, RuntimeType: runtimeType, ContextSupported: contextSupported,
		RuntimeEligible: &eligible, PlatformDependencyAvailable: &dependencyAvailable,
		Available: contextSupported && runtimeFact.Available() && eligible && dependencyAvailable}
}

func Current() Snapshot {
	return CurrentWithEnvironment(ObserveEnvironment())
}

func CurrentWithEnvironment(environment Environment) Snapshot {
	result := Snapshot{Schema: Schema, ComponentProfile: profile.Binary, Facts: []Fact{}, MulticastInterfaces: environment.MulticastNames()}
	for _, fact := range registry.Facts() {
		result.Facts = append(result.Facts, ResolveWithEnvironment(fact.Category, fact.Type, environment))
	}
	for _, mapping := range entitytypes.Mappings() {
		result.Facts = append(result.Facts, ResolveWithEnvironment(mapping.Category, mapping.Type, environment))
	}
	return result
}

// Resolve interprets a captured target snapshot at the same semantic owner as
// the live resolver. Missing or contradictory target facts fail closed.
func (s Snapshot) Resolve(category, panelType string) Fact {
	result := Fact{Fact: registry.Fact{Category: category, Type: panelType, Reason: "UNKNOWN_ENTITY_TYPE"}, RuntimeType: entitytypes.RuntimeType(category, panelType)}
	if s.Schema != Schema {
		result.Reason = "CAPABILITY_SNAPSHOT_UNAVAILABLE"
		return result
	}
	identity := ResolveWithEnvironment(category, panelType, Environment{})
	if !identity.ContextSupported {
		result.Known = identity.Known
		result.Reason = identity.Reason
		return result
	}
	for _, fact := range s.Facts {
		if fact.Category == category && fact.Type == panelType {
			result = fact
			result.RuntimeEligible = copyEligibility(fact.RuntimeEligible)
			result.PlatformDependencyAvailable = copyEligibility(fact.PlatformDependencyAvailable)
			legacy := identity.RuntimeDependency == ""
			result.Available = fact.Known && fact.ContextSupported && fact.Registered && fact.Compiled && fact.SupportedByProduct && fact.Available && fact.RuntimeType == entitytypes.RuntimeType(category, panelType) && fact.RuntimeDependency == identity.RuntimeDependency && optionalEligibility(fact.RuntimeEligible, legacy) && optionalEligibility(fact.PlatformDependencyAvailable, legacy)
			if !result.Available && result.Reason == "" {
				result.Reason = "CAPABILITY_SNAPSHOT_UNAVAILABLE"
			}
			return result
		}
		if fact.Type == panelType {
			result.Known = true
		}
	}
	if result.Known {
		result.Reason = "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT"
	}
	return result
}

func copyEligibility(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
