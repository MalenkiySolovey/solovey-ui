package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func buildFor(source Source) *debug.BuildInfo {
	return &debug.BuildInfo{Deps: []*debug.Module{{Path: EngineModule, Version: "v" + source.RuntimeSemanticVersion, Replace: &debug.Module{
		Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum,
	}}}}
}

func TestRequiredProviderNameIgnoresOptionalComponentTags(t *testing.T) {
	for _, tags := range []string{"shtorm_extended", "shtorm_extended,with_openconnect,with_gvisor", "shtorm_extended,with_openvpn,with_gvisor"} {
		if RequiredProviderName(tags) != "Shtorm Extended" {
			t.Fatal("optional flavor tags erased the provider requirement")
		}
	}
	for _, tags := range []string{"", "with_gvisor", "with_openvpn", "not_shtorm_extended"} {
		if RequiredProviderName(tags) != "" {
			t.Fatal("component tags acquired provider identity")
		}
	}
}

func TestIdentityUsesActualReplacementAndSeparatesArtifactTrust(t *testing.T) {
	for _, source := range pins.Sources {
		t.Run(source.ID, func(t *testing.T) {
			value := Resolve(buildFor(source))
			if value.State != Verified || value.Source != source || value.Revision == "" {
				t.Fatalf("source identity not resolved: %+v", value)
			}
			if value.ArtifactState != Unknown || value.ArtifactDigest != "" {
				t.Fatal("module checksum was mistaken for artifact trust")
			}
			if value.RuntimeSemanticVersion == value.ProviderVersion {
				t.Fatal("provider version must not stand in for semantic version")
			}
		})
	}
}

func TestSourceIdentityVerificationRequiresCompleteOwnerProjection(t *testing.T) {
	for _, source := range pins.Sources {
		if !SourceIdentityVerified(Resolve(buildFor(source))) {
			t.Fatal("owner projection rejected")
		}
	}
	for name, mutate := range map[string]func(*Identity){
		"schema": func(v *Identity) { v.Schema = "unknown" },
		"state":  func(v *Identity) { v.State = Unknown },
		"source": func(v *Identity) {
			v.SourceRevision = "untrusted-source"
			v.Revision = ""
			*v = finish(*v)
		},
		"digest":     func(v *Identity) { v.Revision = "asserted" },
		"projection": func(v *Identity) { v.ReasonCodes = []string{"changed"} },
	} {
		t.Run(name, func(t *testing.T) {
			identity := Resolve(buildFor(pins.Sources[0]))
			mutate(&identity)
			if SourceIdentityVerified(identity) {
				t.Fatal("changed projection accepted")
			}
		})
	}
	if SourceIdentityVerified(Resolve(nil)) || SourceIdentityVerified(Identity{}) {
		t.Fatal("unknown projection accepted")
	}
}

func TestUnknownIdentityCannotMasqueradeAsOfficial(t *testing.T) {
	source := pins.Sources[0]
	cases := map[string]*debug.BuildInfo{"missing metadata": nil, "missing module": {}, "unpatched official": {
		Deps: []*debug.Module{{Path: EngineModule, Version: "v1.14.2", Sum: "unqualified"}},
	}}
	for name, mutate := range map[string]func(*debug.Module){
		"sum":        func(m *debug.Module) { m.Sum = "unverified" },
		"version":    func(m *debug.Module) { m.Version = "v1.14.3" },
		"fork":       func(m *debug.Module) { m.Path = "github.com/deposist/sing-box-extended" },
		"local path": func(m *debug.Module) { m.Path = "../engine"; m.Version = ""; m.Sum = "" },
		"nested replacement": func(m *debug.Module) {
			m.Replace = &debug.Module{Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum}
		},
	} {
		input := buildFor(source)
		mutate(input.Deps[0].Replace)
		cases[name] = input
	}
	duplicate := buildFor(source)
	duplicate.Deps = append(duplicate.Deps, duplicate.Deps[0])
	cases["duplicate engine"] = duplicate
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got := Resolve(input)
			if got.State != Unknown || got.ID != "" || got.SourceRevision != "" || len(got.ReasonCodes) == 0 {
				t.Fatalf("unverified source asserted a provider: %+v", got)
			}
		})
	}
}

func TestOfficialSourcePinMatchesRootLockAndPreservesDefault(t *testing.T) {
	if DefaultID() != "sing-box" {
		t.Fatal("Official default changed")
	}
	mod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join("..", "..", "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range pins.Sources {
		if source.ID != DefaultID() {
			continue
		}
		if !strings.Contains(string(mod), EngineModule+" => "+source.Module+" "+source.ProviderVersion) ||
			!strings.Contains(string(sums), source.Module+" "+source.ProviderVersion+" "+source.ModuleSum) {
			t.Fatal("Official provider identity drifted from root module lock")
		}
	}
}

func TestUnverifiedBuildMetadataIsNeverEchoed(t *testing.T) {
	input := buildFor(pins.Sources[0])
	input.Deps[0].Replace.Path = "credential-sentinel-untrusted-build-metadata"
	encoded, err := json.Marshal(Resolve(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credential-sentinel") {
		t.Fatal("untrusted build metadata echoed")
	}
}

func TestCurrentIdentityDoesNotShareMutableReasons(t *testing.T) {
	first := Current()
	before := append([]string(nil), first.ReasonCodes...)
	first.ReasonCodes[0] = "CALLER_MUTATION"
	second := Current()
	if strings.Join(second.ReasonCodes, ",") != strings.Join(before, ",") || second.Revision != first.Revision {
		t.Fatal("caller mutated cached build identity")
	}
}
