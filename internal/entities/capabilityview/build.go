// Package capabilityview carries bounded, engine-free observations of the
// existing registry and entity capability owners. It owns no registrations.
package capabilityview

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/core/registry/facts"
)

const (
	Schema   = "solovey-ui/entity-build-capabilities/v1"
	MaxBytes = 48 << 10
	MaxFacts = 512
)

type Fact struct {
	facts.Fact
	RuntimeType      string `json:"runtimeType"`
	ContextSupported bool   `json:"contextSupported"`
}

func (f Fact) Supported() bool { return f.ContextSupported && f.Available() }

type State string

const (
	Supported   State = "SUPPORTED"
	Unsupported State = "UNSUPPORTED"
	Unknown     State = "UNKNOWN"
)

func (f Fact) State() State {
	if !f.Known {
		return Unknown
	}
	if f.Supported() {
		return Supported
	}
	return Unsupported
}

type Snapshot struct {
	Schema           string                `json:"schema"`
	ComponentProfile string                `json:"componentProfile"`
	Provider         coreprovider.Identity `json:"provider"`
	Facts            []Fact                `json:"facts"`
}

var token = regexp.MustCompile(`^[A-Za-z0-9._-]{1,96}$`)
var buildTag = regexp.MustCompile(`^[A-Za-z0-9_,.-]{0,256}$`)

// Validate checks the observation's shape, identity and canonical ordering. It
// does not infer runtime availability, permission, or option-value validity.
func (s Snapshot) Validate(provider coreprovider.Identity, profile string) error {
	failed := errors.New("build capability observation is invalid")
	left, _ := json.Marshal(s.Provider)
	right, _ := json.Marshal(provider)
	if s.Schema != Schema || (profile != "full" && profile != "core") || s.ComponentProfile != profile ||
		string(left) != string(right) || len(s.Facts) == 0 || len(s.Facts) > MaxFacts {
		return failed
	}
	for index, fact := range s.Facts {
		if !token.MatchString(fact.Category) || !token.MatchString(fact.Type) || !token.MatchString(fact.RuntimeType) ||
			!buildTag.MatchString(fact.BuildTag) || fact.Platform != "" && !token.MatchString(fact.Platform) ||
			fact.Reason != "" && !token.MatchString(fact.Reason) || fact.RuntimeDependency != "" && !token.MatchString(fact.RuntimeDependency) ||
			!fact.Supported() && fact.Reason == "" {
			return failed
		}
		if index > 0 {
			previous := s.Facts[index-1]
			if cmp.Or(cmp.Compare(previous.Category, fact.Category), cmp.Compare(previous.Type, fact.Type)) >= 0 {
				return failed
			}
		}
	}
	encoded, err := json.Marshal(s)
	if err != nil || len(encoded) > MaxBytes {
		return failed
	}
	return nil
}

func (s Snapshot) Revision() string {
	encoded, _ := json.Marshal(s)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Resolve preserves unknown as unknown; a missing fact cannot authorize use.
func (s Snapshot) Resolve(category, kind string) Fact {
	for _, fact := range s.Facts {
		if fact.Category == category && fact.Type == kind {
			return fact
		}
	}
	return Fact{Fact: facts.Fact{Category: category, Type: kind, Reason: "CAPABILITY_OBSERVATION_UNAVAILABLE"}, RuntimeType: kind}
}
