package release

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
)

// CoreProviderSource reuses the source owner's projection. Signed metadata can
// describe a future immutable source; it is not source/runtime qualification.
// Candidate executable verification must compare the complete source projection.
type CoreProviderSource = coreprovider.Source

var sourceCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)
var buildTag = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var arm64Level = regexp.MustCompile(`^v(?:8\.[0-9]|9\.[0-5])(?:,lse)?(?:,crypto)?$`)

func ValidateCoreProviderSource(source CoreProviderSource) error {
	name, upstream := "Official", "https://github.com/SagerNet/sing-box"
	switch source.ID {
	case "sing-box":
	case "shtorm-extended":
		name, upstream = "Shtorm Extended", "https://github.com/shtorm-7/sing-box-extended"
	default:
		return errors.New("release core provider is unsupported")
	}
	if source.Name != name || !strings.HasPrefix(source.ProviderVersion, "v") ||
		!version.MatchString(strings.TrimPrefix(source.ProviderVersion, "v")) ||
		!version.MatchString(source.RuntimeSemanticVersion) || !sourceCommit.MatchString(source.SourceRevision) ||
		!sourceCommit.MatchString(source.UpstreamRevision) ||
		!oneOf(source.UpstreamRepository, upstream, "https://github.com/SagerNet/sing-box") ||
		!oneOf(source.SourceRepository, upstream, "https://github.com/MalenkiySolovey/sing-box") ||
		source.Module != strings.TrimPrefix(source.SourceRepository, "https://") {
		return errors.New("release core provider source identity is invalid")
	}
	if !validModuleSum(source.ModuleSum) {
		return errors.New("release core provider module checksum is invalid")
	}
	return nil
}

func validModuleSum(sum string) bool {
	checksum, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(sum, "h1:"))
	return strings.HasPrefix(sum, "h1:") && err == nil && len(checksum) == 32
}

func (m Manifest) validateCoreProviders() error {
	if m.Schema == SchemaV1 {
		if len(m.CoreProviders) != 0 {
			return errors.New("legacy release cannot declare core providers")
		}
		for _, artifact := range m.Artifacts {
			if artifact.CoreProvider != "" {
				return errors.New("legacy artifact cannot declare a core provider")
			}
		}
		return nil
	}
	if len(m.CoreProviders) == 0 || len(m.CoreProviders) > 2 {
		return errors.New("release core provider inventory is invalid")
	}
	sources := map[string]bool{}
	for _, source := range m.CoreProviders {
		if err := ValidateCoreProviderSource(source); err != nil {
			return err
		}
		if _, exists := sources[source.ID]; exists {
			return errors.New("release core provider is duplicated")
		}
		sources[source.ID] = false
	}
	names := map[string]bool{}
	roles := map[string]map[string]bool{}
	for _, artifact := range m.Artifacts {
		if names[artifact.Name] {
			return errors.New("release artifact filename is ambiguous")
		}
		names[artifact.Name] = true
		if artifact.Role != "panel-full" && artifact.Role != "panel-core" {
			if artifact.CoreProvider != "" {
				return errors.New("non-panel artifact claims a core provider")
			}
			continue
		}
		if _, exists := sources[artifact.CoreProvider]; !exists {
			return errors.New("panel artifact core provider is undeclared")
		}
		sources[artifact.CoreProvider] = true
		target := artifact.Platform + "\x00" + artifact.Arch + "\x00" + artifact.CoreProvider
		if roles[target] == nil {
			roles[target] = map[string]bool{}
		}
		roles[target][artifact.Role] = true
	}
	for _, used := range sources {
		if !used {
			return errors.New("release core provider has no panel artifacts")
		}
	}
	for _, target := range roles {
		if !target["panel-full"] || !target["panel-core"] {
			return errors.New("release core provider target lacks full/core symmetry")
		}
	}
	return nil
}

func (m Manifest) coreProvider(id string) (CoreProviderSource, bool) {
	for _, source := range m.CoreProviders {
		if source.ID == id {
			return source, true
		}
	}
	return CoreProviderSource{}, false
}

// PanelArtifactsForProvider requires an authenticated unchanged release and an
// explicit selection. No filename, first artifact or default-provider fallback
// can select a v2 artifact. It grants no transition/installation admission.
func (v Verified) PanelArtifactsForProvider(platform, arch, profile, provider string) ([]Artifact, error) {
	if err := v.authenticated(); err != nil {
		return nil, err
	}
	if provider != "sing-box" && provider != "shtorm-extended" {
		return nil, errors.New("release core provider selection is unsupported")
	}
	if v.Manifest.Schema == SchemaV1 {
		if provider != "sing-box" {
			return nil, errors.New("legacy release cannot select Extended")
		}
		return v.Manifest.ArtifactsFor(platform, arch, profile)
	}
	if profile != "full" && profile != "core" {
		return nil, errors.New("release binary profile is unsupported")
	}
	var selected []Artifact
	for _, artifact := range v.Manifest.Artifacts {
		if artifact.Platform == platform && artifact.Arch == arch && artifact.Role == "panel-"+profile && artifact.CoreProvider == provider {
			selected = append(selected, artifact)
		}
	}
	if len(selected) != 1 {
		return nil, errors.New("release core provider target is unavailable or ambiguous")
	}
	return selected, nil
}
