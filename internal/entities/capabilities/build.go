package capabilities

import (
	"cmp"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
)

// Build observes this executable's semantic registrations. Environment facts
// are deliberately absent: target construction and subsequent runtime health
// still have to prove options and external dependencies on the installed Host.
func Build() capabilityview.Snapshot {
	current := CurrentWithEnvironment(Environment{})
	result := capabilityview.Snapshot{Schema: capabilityview.Schema, ComponentProfile: current.ComponentProfile,
		Provider: current.Provider, Facts: make([]capabilityview.Fact, 0, len(current.Facts))}
	for _, fact := range current.Facts {
		if fact.ContextSupported {
			fact.Reason = registry.Resolve(fact.Category, fact.RuntimeType).Reason
		}
		result.Facts = append(result.Facts, capabilityview.Fact{Fact: fact.Fact,
			RuntimeType: fact.RuntimeType, ContextSupported: fact.ContextSupported})
	}
	slices.SortFunc(result.Facts, func(a, b capabilityview.Fact) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Type, b.Type))
	})
	return result
}
