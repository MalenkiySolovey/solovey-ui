package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ArtifactIdentity describes signed content, independently of its transport URL,
// mirror, cache path or download attempt. It is metadata, not admission authority.
type ArtifactIdentity struct {
	ArtifactID     string             `json:"artifactId"`
	Owner          string             `json:"owner"`
	Kind           string             `json:"kind"`
	Version        string             `json:"version"`
	Platform       string             `json:"platform"`
	Arch           string             `json:"arch"`
	ContentDigest  string             `json:"contentDigest"`
	Size           int64              `json:"size"`
	SignerRef      string             `json:"signerRef"`
	ProvenanceRef  string             `json:"provenanceRef"`
	SourceRevision string             `json:"sourceRevision"`
	CoreProvider   CoreProviderSource `json:"coreProvider,omitzero"`
}

type ComponentArtifact struct {
	Component Component        `json:"component"`
	Artifact  ArtifactIdentity `json:"artifact"`
}

// Only Verify can construct this binding. Public Verified metadata cannot be
// edited or assembled from a checksum and then promoted to publisher trust.
type verificationProof struct{ canonical, digest, keyID string }

func (v Verified) authenticated() error {
	if v.proof == nil || v.Digest != v.proof.digest || v.KeyID != v.proof.keyID || string(v.Canonical) != v.proof.canonical {
		return errors.New("release signature binding unavailable")
	}
	canonical, err := Canonical(v.Manifest)
	if err != nil || string(canonical) != v.proof.canonical {
		return errors.New("verified release metadata changed")
	}
	return nil
}

func (v Verified) ArtifactIdentity(artifact Artifact) (ArtifactIdentity, error) {
	if err := v.authenticated(); err != nil {
		return ArtifactIdentity{}, err
	}
	for _, signed := range v.Manifest.Artifacts {
		if signed == artifact {
			return v.identity(signed), nil
		}
	}
	return ArtifactIdentity{}, errors.New("artifact outside verified release")
}

func (v Verified) identity(artifact Artifact) ArtifactIdentity {
	owner := "release"
	switch artifact.Role {
	case "panel-full", "panel-core":
		owner = "panel"
	case "component-catalog":
		owner = "components"
	}
	sum := sha256.Sum256([]byte(artifact.Role + "\x00" + artifact.SHA256))
	identity := ArtifactIdentity{ArtifactID: "artifact:" + hex.EncodeToString(sum[:]), Owner: owner,
		Kind: artifact.Role, Version: v.Manifest.Version, Platform: artifact.Platform, Arch: artifact.Arch,
		ContentDigest: artifact.SHA256, Size: artifact.Size, SignerRef: "release-root:" + v.KeyID,
		ProvenanceRef: artifact.Provenance, SourceRevision: v.Digest}
	if source, ok := v.Manifest.coreProvider(artifact.CoreProvider); ok {
		identity.CoreProvider = source
	}
	return identity
}

func (v Verified) PanelArtifacts(platform, arch, profile string) ([]Artifact, error) {
	if err := v.authenticated(); err != nil {
		return nil, err
	}
	return v.Manifest.ArtifactsFor(platform, arch, profile)
}

// ComponentArtifacts resolves the component catalog already shipped in release
// sets. It does not download/install components or alter compiled inventory.
func (v Verified) ComponentArtifacts() ([]ComponentArtifact, error) {
	if err := v.authenticated(); err != nil {
		return nil, err
	}
	result := make([]ComponentArtifact, 0, len(v.Manifest.Components))
	for _, component := range v.Manifest.Components {
		var matched *Artifact
		for _, artifact := range v.Manifest.Artifacts {
			if artifact.Role == "component-catalog" && artifact.SHA256 == component.ArtifactSHA256 {
				if matched != nil {
					return nil, errors.New("component artifact identity ambiguous")
				}
				copyArtifact := artifact
				matched = &copyArtifact
			}
		}
		if matched == nil {
			return nil, errors.New("component outside signed catalog")
		}
		result = append(result, ComponentArtifact{Component: component, Artifact: v.identity(*matched)})
	}
	return result, nil
}
