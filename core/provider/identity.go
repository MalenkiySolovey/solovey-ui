// Package provider observes the embedded engine's source identity. It owns no
// selection setting, persistence, installation, artifact trust or runtime switch.
package provider

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"runtime/debug"
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
} {
	var value struct {
		Schema          string   `json:"schema"`
		DefaultProvider string   `json:"defaultProvider"`
		Sources         []Source `json:"sources"`
	}
	if err := json.Unmarshal(pinsJSON, &value); err != nil {
		panic("invalid embedded provider source pins")
	}
	return value
}()

func DefaultID() string { return pins.DefaultProvider }

// Current never infers Official from a missing dependency, a version prefix,
// a filename or a user setting. Build metadata must match a complete source pin.
var current = sync.OnceValue(observe)

func Current() Identity {
	identity := current()
	identity.ReasonCodes = append([]string(nil), identity.ReasonCodes...)
	return identity
}

func observe() Identity {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unresolved("BUILD_INFO_UNAVAILABLE")
	}
	return Resolve(info)
}

func Resolve(info *debug.BuildInfo) Identity {
	if info == nil {
		return unresolved("BUILD_INFO_UNAVAILABLE")
	}
	var engine *debug.Module
	for _, module := range info.Deps {
		if module == nil || module.Path != EngineModule {
			continue
		}
		if engine != nil {
			return unresolved("ENGINE_MODULE_AMBIGUOUS")
		}
		engine = module
	}
	if engine == nil {
		return unresolved("ENGINE_MODULE_UNAVAILABLE")
	}
	if engine.Replace != nil {
		engine = engine.Replace
	}
	if engine.Replace != nil {
		return unresolved("ENGINE_REPLACEMENT_UNVERIFIED")
	}
	for _, source := range pins.Sources {
		if engine.Path == source.Module && engine.Version == source.ProviderVersion && engine.Sum == source.ModuleSum {
			return finish(Identity{Schema: Schema, State: Verified, Source: source,
				ArtifactState: Unknown, ReasonCodes: []string{"ARTIFACT_DIGEST_NOT_OBSERVED"}})
		}
	}
	return unresolved("ENGINE_SOURCE_UNVERIFIED")
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
