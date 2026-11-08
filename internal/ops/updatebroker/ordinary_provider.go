package updatebroker

import (
	"encoding/json"
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// OrdinaryUpdateV1 is evidence on the existing Root update checkpoint. It
// records the independently observed installed source and the signed target
// source. Both must belong to the same provider; this is no transition authority.
type OrdinaryUpdateV1 struct {
	OperationID       string                     `json:"operationId"`
	ManifestDigest    string                     `json:"manifestDigest"`
	ArtifactSetDigest string                     `json:"artifactSetDigest"`
	Sequence          uint64                     `json:"sequence"`
	Version           string                     `json:"version"`
	BinaryProfile     string                     `json:"binaryProfile"`
	CoreProvider      release.CoreProviderSource `json:"coreProvider"`
	InstalledProvider release.CoreProviderSource `json:"installedProvider"`
	Revision          string                     `json:"revision"`
}

func ordinaryUpdateBinding(operationID string, identity ReleaseIdentityV1, installed release.CoreProviderSource) OrdinaryUpdateV1 {
	value := OrdinaryUpdateV1{OperationID: operationID, ManifestDigest: identity.ManifestDigest,
		ArtifactSetDigest: identity.ArtifactSetDigest, Sequence: identity.Sequence, Version: identity.Version,
		BinaryProfile: identity.BinaryProfile, CoreProvider: identity.CoreProvider, InstalledProvider: installed}
	value.Revision = ordinaryUpdateRevision(value)
	return value
}

func ordinaryUpdateRevision(value OrdinaryUpdateV1) string {
	value.Revision = ""
	raw, _ := json.Marshal(value)
	return broker.Digest(raw)
}

func ValidateOrdinaryUpdate(value OrdinaryUpdateV1) error {
	if !safeOperationID(value.OperationID) || !validDigest(value.ManifestDigest) || !validDigest(value.ArtifactSetDigest) ||
		value.Sequence == 0 || !versionPattern.MatchString(value.Version) ||
		(value.BinaryProfile != "full" && value.BinaryProfile != "core") ||
		release.ValidateCoreProviderSource(value.CoreProvider) != nil || release.ValidateCoreProviderSource(value.InstalledProvider) != nil ||
		value.CoreProvider.ID != value.InstalledProvider.ID || value.Revision != ordinaryUpdateRevision(value) {
		return errors.New("ordinary update provider binding is invalid")
	}
	return nil
}

func (value OrdinaryUpdateV1) Matches(operationID string, identity ReleaseIdentityV1) bool {
	return ValidateOrdinaryUpdate(value) == nil && value == ordinaryUpdateBinding(operationID, identity, value.InstalledProvider)
}

func (value OrdinaryUpdateV1) observation() *OrdinaryUpdateV1 {
	if value == (OrdinaryUpdateV1{}) {
		return nil
	}
	return &value
}
