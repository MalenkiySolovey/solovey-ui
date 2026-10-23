// Package coreprovider supplies catalog-bound source fixtures for tests whose
// executables omit dependency build metadata. It is not a runtime observer.
package coreprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	provider "github.com/MalenkiySolovey/solovey-ui/core/provider"
)

func Identity(t testing.TB, id string) provider.Identity {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("provider fixture source location unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "..", "..", "core", "provider", "pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		BuildFlavors []struct{ ID, SourceRevision string }
		Sources      []provider.Source
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	var matches []provider.Source
	for _, flavor := range catalog.BuildFlavors {
		for _, source := range catalog.Sources {
			if flavor.ID == id && source.ID == id && source.SourceRevision == flavor.SourceRevision {
				matches = append(matches, source)
			}
		}
	}
	if len(matches) != 1 {
		t.Fatalf("build flavor %s has %d source matches", id, len(matches))
	}
	source := matches[0]
	identity := provider.Resolve(&debug.BuildInfo{Deps: []*debug.Module{{Path: provider.EngineModule,
		Version: "v" + source.RuntimeSemanticVersion, Replace: &debug.Module{
			Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum,
		}}}})
	if !provider.SourceIdentityVerified(identity) {
		t.Fatal("fixture provider identity is not verified")
	}
	return identity
}
