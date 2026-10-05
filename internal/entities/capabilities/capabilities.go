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
	RuntimeType      string `json:"runtimeType"`
	ContextSupported bool   `json:"contextSupported"`
	Available        bool   `json:"available"`
}

type Snapshot struct {
	Schema           string `json:"schema"`
	ComponentProfile string `json:"componentProfile"`
	Facts            []Fact `json:"facts"`
}

func Resolve(category, panelType string) Fact {
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
	return Fact{Fact: runtimeFact, RuntimeType: runtimeType, ContextSupported: contextSupported,
		Available: contextSupported && runtimeFact.Available()}
}

func Current() Snapshot {
	result := Snapshot{Schema: Schema, ComponentProfile: profile.Binary, Facts: []Fact{}}
	for _, fact := range registry.Facts() {
		result.Facts = append(result.Facts, Resolve(fact.Category, fact.Type))
	}
	for _, mapping := range entitytypes.Mappings() {
		result.Facts = append(result.Facts, Resolve(mapping.Category, mapping.Type))
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
	identity := Resolve(category, panelType)
	if !identity.ContextSupported {
		result.Known = identity.Known
		result.Reason = identity.Reason
		return result
	}
	for _, fact := range s.Facts {
		if fact.Category == category && fact.Type == panelType {
			result = fact
			result.Available = fact.Known && fact.ContextSupported && fact.Registered && fact.Compiled && fact.Available && fact.RuntimeType == entitytypes.RuntimeType(category, panelType)
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
