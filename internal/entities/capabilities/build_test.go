package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
)

func TestBuildCapabilityObservationSeparatesBuildFactsFromEnvironment(t *testing.T) {
	observed := Build()
	if err := observed.Validate(observed.Provider, observed.ComponentProfile); err != nil {
		t.Fatal(err)
	}
	if observed.Revision() != Build().Revision() {
		t.Fatal("unchanged build observation has no stable revision")
	}
	encoded, err := json.Marshal(observed)
	if err != nil || len(encoded) >= capabilityview.MaxBytes {
		t.Fatal("build capability evidence exceeded the fixed command contract")
	}
	var dependencyCount int
	for _, fact := range observed.Facts {
		if fact.RuntimeDependency == "" || !fact.ContextSupported {
			continue
		}
		dependencyCount++
		declaration := registry.Resolve(fact.Category, fact.RuntimeType)
		if fact.Fact != declaration {
			t.Fatal("environment readiness replaced the registry's build fact")
		}
		current := ResolveWithEnvironment(fact.Category, fact.Type, Environment{})
		if current.Available {
			t.Fatal("missing runtime environment was promoted by build support")
		}
	}
	if dependencyCount == 0 || observed.Resolve("endpoints", "unknown-provider-protocol").Supported() {
		t.Fatal("build observation lost dependency or unknown-capability semantics")
	}
	t.Logf("facts=%d canonical_bytes=%d revision=%s", len(observed.Facts), len(encoded), observed.Revision())
}

func TestBuildCapabilityObservationRejectsAmbiguousFacts(t *testing.T) {
	for name, mutate := range map[string]func(*capabilityview.Snapshot){
		"missing":                func(s *capabilityview.Snapshot) { s.Facts = nil },
		"duplicate":              func(s *capabilityview.Snapshot) { s.Facts = append(s.Facts, s.Facts[len(s.Facts)-1]) },
		"unordered":              func(s *capabilityview.Snapshot) { s.Facts[0], s.Facts[1] = s.Facts[1], s.Facts[0] },
		"profile":                func(s *capabilityview.Snapshot) { s.ComponentProfile = "unknown" },
		"unknown-without-reason": func(s *capabilityview.Snapshot) { s.Facts[0].Known, s.Facts[0].Reason = false, "" },
	} {
		t.Run(name, func(t *testing.T) {
			observed := Build()
			mutate(&observed)
			if observed.Validate(observed.Provider, observed.ComponentProfile) == nil {
				t.Fatal("incomplete capability observation was accepted")
			}
		})
	}
}
