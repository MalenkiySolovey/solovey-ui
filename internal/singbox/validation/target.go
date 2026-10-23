package validation

import (
	"crypto/sha256"
	"encoding/hex"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

const MaxTargetConfigBytes = targetcheck.MaxConfigBytes
const TargetCheckSchema = targetcheck.Schema

type TargetCheck = targetcheck.Report

// CheckTargetConfig uses this build's existing owner checks, option registries
// and construct/close dry validation. It starts no runtime and writes no Panel
// state. Its result is evidence only: installation must independently bind the
// exact executable to a trusted artifact and a current maintenance checkpoint.
func CheckTargetConfig(raw []byte) TargetCheck {
	return checkTargetConfig(raw, coreprovider.Current())
}

func checkTargetConfig(raw []byte, identity coreprovider.Identity) TargetCheck {
	result := TargetCheck{Schema: TargetCheckSchema, State: "UNAVAILABLE", CoreProvider: identity,
		ReasonCodes: []string{"CORE_PROVIDER_IDENTITY_UNVERIFIED"}}
	if !coreprovider.SourceIdentityVerified(identity) {
		return result
	}
	result.State, result.ReasonCodes = "REJECTED", []string{"CONFIGURATION_INPUT_INVALID"}
	if len(raw) == 0 || len(raw) > MaxTargetConfigBytes || jsonvalue.StrictObject(raw) != nil {
		return result
	}
	digest := sha256.Sum256(raw)
	result.ConfigurationDigest = hex.EncodeToString(digest[:])
	if err := ValidateConfig(raw); err != nil {
		// Owner errors can contain paths or configuration values. A local target
		// check emits a bounded code, never that diagnostic or transformed data.
		result.ReasonCodes = []string{"TARGET_CONFIGURATION_REJECTED"}
		return result
	}
	result.State, result.ReasonCodes = "VALID", []string{}
	return result
}
