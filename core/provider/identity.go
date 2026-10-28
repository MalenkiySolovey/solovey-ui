// Package provider observes the embedded engine's source identity. It owns no
// selection setting, persistence, installation, artifact trust or runtime switch.
package provider

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"runtime/debug"
	"strings"
	"sync"
)

const (
	Schema       = "solovey.core-provider-identity/v1"
	EngineModule = "github.com/sagernet/sing-box"
	Verified     = "VERIFIED"
	Unknown      = "UNKNOWN"
)

// Source describes an immutable, checksum-bound source, not operational support.
// The provider release and the engine semantic version can differ.
type Source struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	ProviderVersion        string `json:"providerVersion"`
	RuntimeSemanticVersion string `json:"runtimeSemanticVersion"`
	Module                 string `json:"module"`
	ModuleSum              string `json:"moduleSum"`
	SourceRepository       string `json:"sourceRepository"`
	SourceRevision         string `json:"sourceRevision"`
	UpstreamRepository     string `json:"upstreamRepository"`
	UpstreamRevision       string `json:"upstreamRevision"`
}

type Identity struct {
	Schema string `json:"schema"`
	State  string `json:"state"`
	Source
	// A module checksum is never an artifact digest. Artifact/Trust must supply
	// independently observed, verified binary identity at the deployment layer.
	ArtifactDigest string   `json:"artifactDigest"`
	ArtifactState  string   `json:"artifactState"`
	ReasonCodes    []string `json:"reasonCodes"`
	Revision       string   `json:"revision"`
}

//go:embed pins.json
var pinsJSON []byte

var pins = func() struct {
	Schema          string   `json:"schema"`
	DefaultProvider string   `json:"defaultProvider"`
	Sources         []Source `json:"sources"`
	BuildFlavors    []struct {
		ID        string   `json:"id"`
		BuildTags []string `json:"buildTags"`
	} `json:"buildFlavors"`
} {
	var value struct {
		Schema          string   `json:"schema"`
		DefaultProvider string   `json:"defaultProvider"`
		Sources         []Source `json:"sources"`
		BuildFlavors    []struct {
			ID        string   `json:"id"`
			BuildTags []string `json:"buildTags"`
		} `json:"buildFlavors"`
	}
	if err := json.Unmarshal(pinsJSON, &value); err != nil {
		panic("invalid embedded provider source pins")
	}
	return value
}()

func DefaultID() string { return pins.DefaultProvider }

// RequiredProviderName projects declared flavor requirements for capability
// explanations. It grants neither install authority nor operational support.
func RequiredProviderName(buildTags string) string {
	tags := "," + buildTags + ","
	for _, flavor := range pins.BuildFlavors {
		if len(flavor.BuildTags) == 0 {
			continue
		}
		matches := true
		for _, tag := range flavor.BuildTags {
			matches = matches && strings.Contains(tags, ","+tag+",")
		}
		if matches {
			for _, source := range pins.Sources {
				if source.ID == flavor.ID {
					return source.Name
				}
			}
		}
	}
	return ""
}

// Current never infers Official from a missing dependency, a version prefix,
// a filename or a user setting. Build metadata must match a complete source pin.
var current = sync.OnceValue(observe)

func Current() Identity {
	identity := current()
	identity.ReasonCodes = append([]string(nil), identity.ReasonCodes...)
	return identity
}

// SourceIdentityVerified checks a complete source projection from this owner.
// It grants no artifact trust or installation authority.
func SourceIdentityVerified(identity Identity) bool {
	if identity.Schema != Schema || identity.State != Verified || identity.Revision == "" {
		return false
	}
	for _, source := range pins.Sources {
		if identity.Source == source {
			revision := identity.Revision
			identity.Revision = ""
			return finish(identity).Revision == revision
		}
	}
	return false
}

func observe() Identity {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unresolved("BUILD_INFO_UNAVAILABLE")
	}
	return Resolve(info)
}

func Resolve(info *debug.BuildInfo) Identity {
	engine, reason := engineDependency(info)
	if reason != "" {
		return unresolved(reason)
	}
	for _, source := range pins.Sources {
		if moduleSourceMatches(engine, source) {
			return finish(Identity{Schema: Schema, State: Verified, Source: source,
				ArtifactState: Unknown, ReasonCodes: []string{"ARTIFACT_DIGEST_NOT_OBSERVED"}})
		}
	}
	return unresolved("ENGINE_SOURCE_UNVERIFIED")
}

// ModuleSourceMatches compares observed module facts with a separately supplied
// immutable source declaration. It grants no source qualification, publisher
// trust or installation authority. Unlike Resolve, it does not require that a
// future signed source already appear in this process's compiled catalog.
func ModuleSourceMatches(info *debug.BuildInfo, source Source) bool {
	engine, reason := engineDependency(info)
	return reason == "" && moduleSourceMatches(engine, source)
}

// ResolveModule observes an exact linked module tuple in the compiled catalog.
// It has no original engine-requirement fact. Callers reading build metadata
// use Resolve; capability owners separately check their observed requirement.
// This projection grants no artifact trust or capability qualification.
func ResolveModule(module, version, sum string) Identity {
	for _, source := range pins.Sources {
		if module == source.Module && version == source.ProviderVersion && sum == source.ModuleSum {
			return finish(Identity{Schema: Schema, State: Verified, Source: source,
				ArtifactState: Unknown, ReasonCodes: []string{"ARTIFACT_DIGEST_NOT_OBSERVED"}})
		}
	}
	return unresolved("ENGINE_SOURCE_UNVERIFIED")
}

func engineDependency(info *debug.BuildInfo) (*debug.Module, string) {
	if info == nil {
		return nil, "BUILD_INFO_UNAVAILABLE"
	}
	var engine *debug.Module
	for _, module := range info.Deps {
		if module == nil || module.Path != EngineModule {
			continue
		}
		if engine != nil {
			return nil, "ENGINE_MODULE_AMBIGUOUS"
		}
		engine = module
	}
	if engine == nil {
		return nil, "ENGINE_MODULE_UNAVAILABLE"
	}
	if engine.Replace != nil && engine.Replace.Replace != nil {
		return nil, "ENGINE_REPLACEMENT_UNVERIFIED"
	}
	return engine, ""
}

func moduleSourceMatches(engine *debug.Module, source Source) bool {
	if engine == nil || source.Module == "" || source.ProviderVersion == "" || source.ModuleSum == "" ||
		source.RuntimeSemanticVersion == "" || engine.Version != "v"+source.RuntimeSemanticVersion {
		return false
	}
	if engine.Replace != nil {
		engine = engine.Replace
	}
	return engine.Replace == nil && engine.Path == source.Module && engine.Version == source.ProviderVersion && engine.Sum == source.ModuleSum
}

func unresolved(reason string) Identity {
	return finish(Identity{Schema: Schema, State: Unknown, ArtifactState: Unknown,
		ReasonCodes: []string{reason, "ARTIFACT_DIGEST_NOT_OBSERVED"}})
}

func finish(identity Identity) Identity {
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	identity.Revision = hex.EncodeToString(digest[:])
	return identity
}
