package release

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func panelBuildFixture(t *testing.T, provider string) (*debug.BuildInfo, CoreProviderSource) {
	t.Helper()
	source := providerfixture.Identity(t, provider).Source
	tags := "with_quic,with_utls"
	if provider == "shtorm-extended" {
		tags += ",shtorm_extended"
	}
	return &debug.BuildInfo{Path: "command-line-arguments", Deps: []*debug.Module{
		{Path: PanelModule, Version: "(devel)"},
		{Path: coreprovider.EngineModule, Version: "v" + source.RuntimeSemanticVersion,
			Replace: &debug.Module{Path: source.Module, Version: source.ProviderVersion, Sum: source.ModuleSum}},
	}, Settings: []debug.BuildSetting{
		{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "amd64"},
		{Key: "GOAMD64", Value: "v1"}, {Key: "-buildmode", Value: "exe"}, {Key: "-tags", Value: tags},
	}}, source
}

func setBuildSetting(info *debug.BuildInfo, key, value string) {
	for i := range info.Settings {
		if info.Settings[i].Key == key {
			info.Settings[i].Value = value
			return
		}
	}
	info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
}

func TestPanelBuildObservationBindsBothProviderABIsAndProfiles(t *testing.T) {
	for _, provider := range []string{"sing-box", "shtorm-extended"} {
		for _, profile := range []string{"full", "core"} {
			t.Run(provider+"/"+profile, func(t *testing.T) {
				info, source := panelBuildFixture(t, provider)
				if profile == "core" {
					info.Settings[len(info.Settings)-1].Value += ",minimal"
				}
				before, _ := json.Marshal(info)
				observed, err := inspectPanelBuild(info, source)
				after, _ := json.Marshal(info)
				if err != nil || observed.CoreProvider != source || observed.Platform != "linux" || observed.Arch != "amd64" ||
					observed.ArchitectureLevel != "v1" || observed.BinaryProfile != profile || !digest.MatchString(observed.ModuleGraphDigest) || !bytes.Equal(before, after) {
					t.Fatalf("immutable observation failed: %+v %v", observed, err)
				}
				slices.Reverse(info.Deps)
				slices.Reverse(info.Settings)
				reordered, err := inspectPanelBuild(info, source)
				if err != nil || !reflect.DeepEqual(observed, reordered) {
					t.Fatal("linked dependency/build setting order affected observation")
				}
				info.Deps[0].Replace.Sum = "h1:" + strings.Repeat("A", 43) + "="
				if _, err := inspectPanelBuild(info, source); err == nil {
					t.Fatal("source module checksum drift was ignored")
				}
			})
		}
	}
}

func TestPanelBuildTargetAndFirstPartyForms(t *testing.T) {
	for _, target := range []struct{ goos, arch, levelKey, level, wantArch string }{
		{"linux", "386", "GO386", "sse2", "386"}, {"linux", "amd64", "GOAMD64", "v3", "amd64"},
		{"linux", "arm", "GOARM", "5,softfloat", "armv5"}, {"linux", "arm", "GOARM", "6,hardfloat", "armv6"},
		{"linux", "arm", "GOARM", "7,hardfloat", "armv7"}, {"linux", "arm64", "GOARM64", "v8.0", "arm64"},
		{"linux", "mipsle", "GOMIPS", "softfloat", "mipsle"}, {"linux", "s390x", "", "", "s390x"},
		{"windows", "amd64", "GOAMD64", "v1", "amd64"}, {"windows", "arm64", "GOARM64", "v8.0", "arm64"},
	} {
		t.Run(target.goos+"/"+target.wantArch, func(t *testing.T) {
			info, source := panelBuildFixture(t, "sing-box")
			setBuildSetting(info, "GOOS", target.goos)
			setBuildSetting(info, "GOARCH", target.arch)
			if target.levelKey != "" {
				setBuildSetting(info, target.levelKey, target.level)
			}
			observed, err := inspectPanelBuild(info, source)
			if err != nil || observed.Platform != target.goos || observed.Arch != target.wantArch || observed.ArchitectureLevel != target.level {
				t.Fatalf("target observation=%+v err=%v", observed, err)
			}
		})
	}
	info, source := panelBuildFixture(t, "sing-box")
	info.Path, info.Main = PanelModule, debug.Module{Path: PanelModule, Version: "(devel)"}
	info.Deps = info.Deps[1:]
	if _, err := inspectPanelBuild(info, source); err != nil {
		t.Fatal("go build . first-party metadata rejected", err)
	}
}

func TestPanelBuildRejectsAmbiguousMissingAndUnpinnedMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*debug.BuildInfo, *CoreProviderSource){
		"wrong-source":          func(_ *debug.BuildInfo, s *CoreProviderSource) { s.ProviderVersion = "v1.14.4-credential-sentinel" },
		"source-shape":          func(_ *debug.BuildInfo, s *CoreProviderSource) { s.SourceRevision = "credential-sentinel" },
		"wrong-main":            func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Path = "credential-sentinel" },
		"no-first-party":        func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps = i.Deps[1:] },
		"duplicate-first-party": func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Main.Path = PanelModule },
		"replaced-first-party":  func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps[0].Replace = &debug.Module{} },
		"first-party-version":   func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps[0].Version = "v1.0.0" },
		"nil-module":            func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps = append(i.Deps, nil) },
		"duplicate-engine":      func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps = append(i.Deps, i.Deps[1]) },
		"nested-replacement":    func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps[1].Replace.Replace = &debug.Module{} },
		"missing-dependency-sum": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			i.Deps = append(i.Deps, &debug.Module{Path: "example.invalid/unpinned", Version: "v1.0.0"})
		},
		"local-dependency": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			i.Deps = append(i.Deps, &debug.Module{Path: "example.invalid/local", Replace: &debug.Module{Path: "../credential-sentinel"}})
		},
		"require-semantic-drift": func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps[1].Version = "v9.9.9" },
		"duplicate-setting":      func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Settings = append(i.Settings, i.Settings[0]) },
		"wrong-platform":         func(i *debug.BuildInfo, _ *CoreProviderSource) { setBuildSetting(i, "GOOS", "credential-sentinel") },
		"missing-arch":           func(i *debug.BuildInfo, _ *CoreProviderSource) { setBuildSetting(i, "GOARCH", "") },
		"invalid-arm": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			setBuildSetting(i, "GOARCH", "arm")
			setBuildSetting(i, "GOARM", "8")
		},
		"shared-library": func(i *debug.BuildInfo, _ *CoreProviderSource) { setBuildSetting(i, "-buildmode", "c-shared") },
		"wrong-ABI":      func(i *debug.BuildInfo, _ *CoreProviderSource) { setBuildSetting(i, "-tags", "shtorm_extended") },
		"duplicate-tag":  func(i *debug.BuildInfo, _ *CoreProviderSource) { setBuildSetting(i, "-tags", "minimal,minimal") },
		"invalid-tag": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			setBuildSetting(i, "-tags", "with_quic,credential-sentinel")
		},
		"too-many-settings":     func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Settings = make([]debug.BuildSetting, 257) },
		"too-many-dependencies": func(i *debug.BuildInfo, _ *CoreProviderSource) { i.Deps = make([]*debug.Module, 4097) },
		"too-many-tags": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			setBuildSetting(i, "-tags", strings.Repeat("x,", 257))
		},
		"oversized-level": func(i *debug.BuildInfo, _ *CoreProviderSource) {
			setBuildSetting(i, "GOAMD64", strings.Repeat("x", 65))
		},
	} {
		t.Run(name, func(t *testing.T) {
			info, source := panelBuildFixture(t, "sing-box")
			mutate(info, &source)
			if _, err := inspectPanelBuild(info, source); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("invalid executable metadata accepted or echoed")
			}
		})
	}
	if _, err := inspectPanelBuild(nil, CoreProviderSource{}); err == nil {
		t.Fatal("nil build metadata accepted")
	}
	if _, err := InspectPanelBuild(nil, CoreProviderSource{}); err == nil {
		t.Fatal("nil executable accepted")
	}
	if _, err := InspectPanelBuild(bytes.NewReader([]byte("credential-sentinel")), CoreProviderSource{}); err == nil || strings.Contains(err.Error(), "credential-sentinel") {
		t.Fatal("non-executable accepted or echoed")
	}
}

func TestSignedPanelInspectionRequiresExactAuthenticatedArtifact(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verified := verifiedIdentityFixture(t, providerManifest(t, now), now)
	artifact := verified.Manifest.Artifacts[0]
	artifact.SHA256 = strings.Repeat("0", 64)
	if _, err := verified.InspectPanelArtifact(nil, artifact); err == nil {
		t.Fatal("changed artifact retained signature binding")
	}
	if _, err := (Verified{Manifest: verified.Manifest}).InspectPanelArtifact(nil, verified.Manifest.Artifacts[0]); err == nil {
		t.Fatal("metadata-only release manufactured executable admission")
	}
	if _, err := verified.InspectPanelArtifact(nil, verified.Manifest.Artifacts[2]); err == nil {
		t.Fatal("component catalog accepted as panel executable")
	}
	legacy := verifiedIdentityFixture(t, validManifest(now), now)
	if _, err := legacy.InspectPanelArtifact(nil, legacy.Manifest.Artifacts[0]); err == nil {
		t.Fatal("legacy artifact invented signed source provenance")
	}
}
