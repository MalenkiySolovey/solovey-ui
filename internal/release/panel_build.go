package release

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"runtime/debug"
	"slices"
	"strings"
	"unicode"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
)

const PanelModule = "github.com/MalenkiySolovey/solovey-ui"

// PanelBuildIdentity observes a candidate without executing it. Its source is
// bound to the supplied declaration's exact module tuple, not to a filename.
// ModuleGraphDigest covers linked module metadata, not the full resolved SBOM.
// The caller must independently bind/revalidate the opened object's bytes.
type PanelBuildIdentity struct {
	CoreProvider      CoreProviderSource `json:"coreProvider"`
	Platform          string             `json:"platform"`
	Arch              string             `json:"arch"`
	BinaryProfile     string             `json:"binaryProfile"`
	ArchitectureLevel string             `json:"architectureLevel,omitempty"`
	BuildTags         []string           `json:"buildTags"`
	ModuleGraphDigest string             `json:"moduleGraphDigest"`
}

func InspectPanelBuild(reader io.ReaderAt, source CoreProviderSource) (PanelBuildIdentity, error) {
	if reader == nil {
		return PanelBuildIdentity{}, errors.New("panel executable metadata is unavailable")
	}
	info, err := buildinfo.Read(reader)
	if err != nil {
		return PanelBuildIdentity{}, errors.New("panel executable Go metadata is invalid")
	}
	observed, err := inspectPanelBuild(info, source)
	if err != nil {
		return PanelBuildIdentity{}, err
	}
	platform, arch, err := executableTarget(reader)
	goArch := observed.Arch
	if oneOf(goArch, "armv5", "armv6", "armv7") {
		goArch = "arm"
	}
	if err != nil || platform != observed.Platform || arch != goArch {
		return PanelBuildIdentity{}, errors.New("panel executable machine differs from its Go target metadata")
	}
	return observed, nil
}

func inspectPanelBuild(info *debug.BuildInfo, source CoreProviderSource) (PanelBuildIdentity, error) {
	if ValidateCoreProviderSource(source) != nil || info == nil ||
		!coreprovider.ModuleSourceMatches(info, source) ||
		(info.Path != PanelModule && info.Path != "command-line-arguments") ||
		len(info.Settings) > 256 || len(info.Deps) == 0 || len(info.Deps) > 4096 {
		return PanelBuildIdentity{}, errors.New("panel executable source metadata does not match")
	}
	firstParty := info.Main.Path == PanelModule && info.Main.Replace == nil
	seenModules := map[string]bool{}
	modules := slices.Clone(info.Deps)
	for _, module := range modules {
		if module == nil || module.Path == "" || seenModules[module.Path] {
			return PanelBuildIdentity{}, errors.New("panel executable module inventory is ambiguous")
		}
		seenModules[module.Path] = true
		if module.Path == PanelModule {
			if firstParty || module.Replace != nil || module.Version != "(devel)" {
				return PanelBuildIdentity{}, errors.New("panel executable first-party module is ambiguous")
			}
			firstParty = true
			continue
		}
		actual := module
		if actual.Replace != nil {
			actual = actual.Replace
		}
		if actual.Replace != nil || actual.Path == "" || actual.Version == "" || actual.Sum == "" ||
			strings.HasPrefix(actual.Path, ".") || strings.ContainsAny(actual.Path, "\\ \t\r\n") ||
			!strings.HasPrefix(actual.Version, "v") || !validModuleSum(actual.Sum) {
			return PanelBuildIdentity{}, errors.New("panel executable has an unpinned dependency")
		}
	}
	if !firstParty {
		return PanelBuildIdentity{}, errors.New("panel executable first-party module is missing")
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		if _, exists := settings[setting.Key]; exists {
			return PanelBuildIdentity{}, errors.New("panel executable build settings are ambiguous")
		}
		settings[setting.Key] = setting.Value
	}
	platform, arch := settings["GOOS"], settings["GOARCH"]
	if !oneOf(platform, "linux", "windows") || !identifier.MatchString(arch) || len(arch) > 16 ||
		settings["-buildmode"] != "exe" || len(settings["-tags"]) > 4096 {
		return PanelBuildIdentity{}, errors.New("panel executable target metadata is invalid")
	}
	level := ""
	switch arch {
	case "arm":
		arm, _, _ := strings.Cut(settings["GOARM"], ",")
		if !oneOf(arm, "5", "6", "7") {
			return PanelBuildIdentity{}, errors.New("panel executable ARM target is invalid")
		}
		arch, level = "armv"+arm, settings["GOARM"]
	case "amd64":
		level = settings["GOAMD64"]
	case "386":
		level = settings["GO386"]
	case "arm64":
		level = settings["GOARM64"]
	case "mips", "mipsle":
		level = settings["GOMIPS"]
	case "mips64", "mips64le":
		level = settings["GOMIPS64"]
	case "ppc64", "ppc64le":
		level = settings["GOPPC64"]
	case "riscv64":
		level = settings["GORISCV64"]
	}
	if !validArchitectureLevel(arch, level) {
		return PanelBuildIdentity{}, errors.New("panel executable architecture level is invalid")
	}
	tags := strings.FieldsFunc(settings["-tags"], func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(tags) > 256 {
		return PanelBuildIdentity{}, errors.New("panel executable tag inventory is invalid")
	}
	seenTags := map[string]bool{}
	for _, tag := range tags {
		if !buildTag.MatchString(tag) || seenTags[tag] {
			return PanelBuildIdentity{}, errors.New("panel executable build tag is ambiguous or invalid")
		}
		seenTags[tag] = true
	}
	if seenTags["shtorm_extended"] != (source.ID == "shtorm-extended") {
		return PanelBuildIdentity{}, errors.New("panel executable provider ABI tag conflicts with source")
	}
	profile := "full"
	if seenTags["minimal"] {
		profile = "core"
	}
	slices.Sort(tags)
	slices.SortFunc(modules, func(left, right *debug.Module) int { return strings.Compare(left.Path, right.Path) })
	encoded, _ := json.Marshal(modules)
	digest := sha256.Sum256(encoded)
	return PanelBuildIdentity{CoreProvider: source, Platform: platform, Arch: arch, BinaryProfile: profile,
		ArchitectureLevel: level, BuildTags: tags, ModuleGraphDigest: hex.EncodeToString(digest[:])}, nil
}

func validArchitectureLevel(arch, level string) bool {
	switch arch {
	case "amd64":
		return oneOf(level, "v1", "v2", "v3", "v4")
	case "386":
		return oneOf(level, "sse2", "softfloat")
	case "armv5", "armv6", "armv7":
		return oneOf(level, strings.TrimPrefix(arch, "armv"), strings.TrimPrefix(arch, "armv")+",hardfloat", strings.TrimPrefix(arch, "armv")+",softfloat")
	case "arm64":
		return arm64Level.MatchString(level)
	case "mips", "mipsle", "mips64", "mips64le":
		return oneOf(level, "hardfloat", "softfloat")
	case "ppc64", "ppc64le":
		return oneOf(level, "power8", "power9", "power10")
	case "riscv64":
		return oneOf(level, "rva20u64", "rva22u64", "rva23u64")
	default:
		return level == ""
	}
}

// InspectPanelArtifact first checks the private signature binding and exact
// signed artifact. Callers still own archive/member digest and object stability
// checks; this function never opens a path or executes a candidate.
func (v Verified) InspectPanelArtifact(reader io.ReaderAt, artifact Artifact) (PanelBuildIdentity, error) {
	identity, err := v.ArtifactIdentity(artifact)
	if err != nil || identity.Owner != "panel" || identity.CoreProvider.ID == "" {
		return PanelBuildIdentity{}, errors.New("signed panel provider identity is unavailable")
	}
	observed, err := InspectPanelBuild(reader, identity.CoreProvider)
	if err != nil || observed.Platform != artifact.Platform || observed.Arch != artifact.Arch ||
		"panel-"+observed.BinaryProfile != artifact.Role {
		return PanelBuildIdentity{}, errors.New("panel executable differs from its signed provider, target or profile")
	}
	return observed, nil
}
