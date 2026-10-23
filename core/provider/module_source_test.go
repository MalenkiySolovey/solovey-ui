package provider

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestModuleSourceComparisonDoesNotGrantFutureSourceQualification(t *testing.T) {
	source := pins.Sources[0]
	source.SourceRevision = strings.Repeat("a", 40)
	source.ProviderVersion = "v1.14.3-0.20261024000000-aaaaaaaaaaaa"
	info := buildFor(source)
	if !ModuleSourceMatches(info, source) {
		t.Fatal("matching future immutable module declaration rejected")
	}
	if Resolve(info).State != Unknown || SourceIdentityVerified(Resolve(info)) {
		t.Fatal("matching a supplied module declaration granted current catalog qualification")
	}
	for name, mutate := range map[string]func(*debug.BuildInfo, *Source){
		"require-semantic":       func(i *debug.BuildInfo, _ *Source) { i.Deps[0].Version = "v9.9.9" },
		"module-path":            func(i *debug.BuildInfo, _ *Source) { i.Deps[0].Replace.Path = "../local" },
		"module-version":         func(i *debug.BuildInfo, _ *Source) { i.Deps[0].Replace.Version = "v9.9.9" },
		"module-sum":             func(i *debug.BuildInfo, _ *Source) { i.Deps[0].Replace.Sum = "unverified" },
		"nested":                 func(i *debug.BuildInfo, _ *Source) { i.Deps[0].Replace.Replace = &debug.Module{} },
		"duplicate":              func(i *debug.BuildInfo, _ *Source) { i.Deps = append(i.Deps, i.Deps[0]) },
		"missing-source-version": func(_ *debug.BuildInfo, s *Source) { s.ProviderVersion = "" },
		"missing-semantic":       func(_ *debug.BuildInfo, s *Source) { s.RuntimeSemanticVersion = "" },
		"missing-source-sum":     func(_ *debug.BuildInfo, s *Source) { s.ModuleSum = "" },
	} {
		t.Run(name, func(t *testing.T) {
			copySource := source
			info := buildFor(copySource)
			mutate(info, &copySource)
			if ModuleSourceMatches(info, copySource) {
				t.Fatal("module tuple drift or ambiguity accepted")
			}
		})
	}
	if ModuleSourceMatches(nil, source) || ModuleSourceMatches(&debug.BuildInfo{Deps: []*debug.Module{nil}}, source) {
		t.Fatal("unavailable module metadata matched")
	}
}

func TestCatalogModuleTupleDoesNotPretendToObserveAnEngineRequirement(t *testing.T) {
	for _, source := range pins.Sources {
		projected := ResolveModule(source.Module, source.ProviderVersion, source.ModuleSum)
		if !SourceIdentityVerified(projected) || projected.Source != source || projected.ArtifactState != Unknown {
			t.Fatal("exact catalog tuple projection lost source facts")
		}
		metadata := buildFor(source)
		metadata.Deps[0].Version = ""
		if Resolve(metadata).State != Unknown || ModuleSourceMatches(metadata, source) {
			t.Fatal("tuple projection bypassed the full build-info requirement check")
		}
	}
	for _, tuple := range [][3]string{
		{"../credential-sentinel", "v1.0.0", "h1:unknown"},
		{pins.Sources[0].Module, pins.Sources[0].ProviderVersion, "unknown"},
		{pins.Sources[0].Module, "v9.9.9", pins.Sources[0].ModuleSum},
	} {
		if identity := ResolveModule(tuple[0], tuple[1], tuple[2]); identity.State != Unknown || identity.ID != "" {
			t.Fatal("unknown module tuple acquired a source projection")
		}
	}
}
