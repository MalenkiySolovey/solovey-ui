package coreinboundcontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
)

func providerSources(t *testing.T) []coreprovider.Source {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "core", "provider", "pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct{ Sources []coreprovider.Source }
	if err := json.Unmarshal(content, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog.Sources
}

func providerBuildInput(source coreprovider.Source, withUTLS bool) RuntimeBuildInputV1 {
	input := exactBuildInput(withUTLS)
	input.Modules[0] = RuntimeModuleV1{Path: PinnedSingBoxModule, Version: "v" + source.RuntimeSemanticVersion,
		Replaced: true, Replacement: &RuntimeModuleV1{Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum}}
	return input
}

func TestQualifiedProviderIdentityAndQUICFactUseTheObservedSource(t *testing.T) {
	qualified := 0
	for _, source := range providerSources(t) {
		t.Run(source.SourceRevision, func(t *testing.T) {
			input := providerBuildInput(source, true)
			identity := ResolveRuntimeIdentityV1(input)
			if !fallbackSourceQualified(observedProviderSource(source.Module, source.ProviderVersion, source.ModuleSum)) {
				if identity.State != RuntimeIdentityUnknown || !containsReason(identity.ReasonCodes, ReasonSingBoxSourceUnqualified) {
					t.Fatal("research source acquired capability qualification")
				}
				return
			}
			qualified++
			if !RuntimeIdentityVerifiedV1(identity) || identity.SingBoxModule != source.Module || identity.SingBoxVersion != source.ProviderVersion ||
				identity.SingBoxModuleSum != source.ModuleSum || identity.SingBoxSourceRevision != source.SourceRevision {
				t.Fatalf("actual source tuple lost: %+v", identity)
			}
			feature := ReadQUICBuildFeatureV1(identity)
			if feature.State == BuildFeatureUnknown || feature.SourceRevision != source.SourceRevision || feature.ModuleRevision == "" {
				t.Fatalf("provider build fact lost source: %+v", feature)
			}
			if identity.IdentityRevision == exactIdentity(true).IdentityRevision {
				t.Fatal("maintained source reused the original runtime identity")
			}
		})
	}
	if qualified != 2 {
		t.Fatalf("qualified source count=%d; source changes require requalification", qualified)
	}
}

func TestBuildInfoReaderPreservesReplacementAndRejectsNestedReplacement(t *testing.T) {
	for _, source := range providerSources(t) {
		if source.ID != "sing-box" {
			continue
		}
		info := &debug.BuildInfo{Deps: []*debug.Module{
			{Path: PinnedSingBoxModule, Version: "v" + source.RuntimeSemanticVersion, Replace: &debug.Module{Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum}},
			{Path: PinnedUTLSModule, Version: PinnedUTLSVersion, Sum: PinnedUTLSModuleSum},
		}}
		input := runtimeInputFromBuildInfo(info)
		if identity := ResolveRuntimeIdentityV1(input); !RuntimeIdentityVerifiedV1(identity) {
			t.Fatalf("observed replacement was not verified: %+v", identity)
		}
		info.Deps[0].Replace.Replace = &debug.Module{Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum}
		if identity := ResolveRuntimeIdentityV1(runtimeInputFromBuildInfo(info)); RuntimeIdentityVerifiedV1(identity) || identity.State != RuntimeIdentityUnknown {
			t.Fatal("nested replacement acquired a verified runtime")
		}
	}
}

func TestProviderIdentityRejectsDriftAmbiguityAndUntrustedMetadata(t *testing.T) {
	var source coreprovider.Source
	for _, candidate := range providerSources(t) {
		if candidate.ID == "shtorm-extended" && fallbackSourceQualified(observedProviderSource(candidate.Module, candidate.ProviderVersion, candidate.ModuleSum)) {
			source = candidate
		}
	}
	if source.Module == "" {
		t.Fatal("native source fixture missing")
	}
	for name, mutate := range map[string]func(*RuntimeBuildInputV1){
		"required semantic version": func(input *RuntimeBuildInputV1) { input.Modules[0].Version = "v1.14.2" },
		"source sum":                func(input *RuntimeBuildInputV1) { input.Modules[0].Replacement.Sum = "h1:unverified" },
		"source version": func(input *RuntimeBuildInputV1) {
			input.Modules[0].Replacement.Version = "untrusted-credential-sentinel"
		},
		"local source": func(input *RuntimeBuildInputV1) {
			input.Modules[0].Replacement.Path = "../untrusted-credential-sentinel"
		},
		"ambiguous engine": func(input *RuntimeBuildInputV1) { input.Modules = append(input.Modules, input.Modules[0]) },
		"ambiguous utls":   func(input *RuntimeBuildInputV1) { input.Modules = append(input.Modules, input.Modules[1]) },
		"unknown utls":     func(input *RuntimeBuildInputV1) { input.Modules[1].Version = "untrusted-credential-sentinel" },
	} {
		t.Run(name, func(t *testing.T) {
			input := providerBuildInput(source, true)
			mutate(&input)
			identity := ResolveRuntimeIdentityV1(input)
			if identity.State != RuntimeIdentityUnknown || RuntimeIdentityVerifiedV1(identity) || ReadQUICBuildFeatureV1(identity).State != BuildFeatureUnknown {
				t.Fatal("unverified source acquired runtime capability")
			}
			encoded, _ := json.Marshal(identity)
			if strings.Contains(string(encoded), "untrusted-credential-sentinel") {
				t.Fatal("untrusted build metadata escaped into diagnostics")
			}
		})
	}
}

func TestRuntimeIdentityDigestIsRequiredForCapabilityAdmission(t *testing.T) {
	identity := exactIdentity(true)
	identity.IdentityRevision = strings.Repeat("f", 64)
	if RuntimeIdentityVerifiedV1(identity) || ReadQUICBuildFeatureV1(identity).State != BuildFeatureUnknown {
		t.Fatal("caller-provided verified label bypassed identity validation")
	}
	identity = exactIdentity(true)
	identity.SingBoxSourceRevision = strings.Repeat("f", 40)
	identity.IdentityRevision = runtimeIdentityRevision(identity)
	if RuntimeIdentityVerifiedV1(identity) {
		t.Fatal("an internally consistent digest qualified an unknown source")
	}
}

func TestRuntimeWithoutUTLSDistinguishesAnAbsentModuleFromUnknownSource(t *testing.T) {
	input := exactBuildInput(false)
	input.Modules = input.Modules[:1]
	identity := ResolveRuntimeIdentityV1(input)
	if !RuntimeIdentityVerifiedV1(identity) || identity.WithUTLS || identity.UTLSModule != "" {
		t.Fatalf("no-uTLS profile was not represented accurately: %+v", identity)
	}
	input.WithUTLS, input.BuildProfileRevision = true, expectedBuildProfileRevision(true)
	if identity := ResolveRuntimeIdentityV1(input); identity.State != RuntimeIdentityUnknown || !containsReason(identity.ReasonCodes, ReasonUTLSModuleMissing) {
		t.Fatal("enabled uTLS without its observed module was admitted")
	}
}
