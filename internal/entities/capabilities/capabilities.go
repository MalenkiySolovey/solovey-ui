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
