package updatebroker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
)

var (
	safeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$`)
	safeIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+-]{0,127}$`)
	digestPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	versionPattern  = regexp.MustCompile(`^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(?:-[0-9A-Za-z.-]{1,64})?$`)
)

func ValidateRelease(value ReleaseIdentityV1) error {
	if !safeIDPattern.MatchString(value.ReleaseID) || value.Sequence == 0 || !versionPattern.MatchString(value.Version) ||
		!digestPattern.MatchString(value.ManifestDigest) ||
		!digestPattern.MatchString(value.ArtifactSetDigest) || !digestPattern.MatchString(value.DeploymentRevision) ||
		!digestPattern.MatchString(value.MigrationSetDigest) || (value.RestartClass != "panel" && value.RestartClass != "stack") ||
		(value.RollbackClass != "automatic" && value.RollbackClass != "manual-recovery") ||
		(value.BinaryProfile != "full" && value.BinaryProfile != "core") || len(value.Artifacts) > 1 {
		return errors.New("update release identity is invalid")
	}
	seen := map[string]struct{}{}
	for _, artifact := range value.Artifacts {
		if err := ValidateArtifact(artifact); err != nil {
			return err
		}
		identity := artifact.Role + "\x00" + artifact.Name
		if _, exists := seen[identity]; exists {
			return errors.New("update artifact identity is duplicated")
		}
		seen[identity] = struct{}{}
		if artifact.Role != "panel-"+value.BinaryProfile {
			return errors.New("update artifact does not match the binary profile")
		}
	}
	if len(value.Artifacts) > 0 && artifactSetDigest(value.Artifacts) != value.ArtifactSetDigest {
		return errors.New("update artifact set digest is invalid")
	}
	return nil
}

func ValidateArtifact(value ArtifactIdentityV1) error {
	if !safeNamePattern.MatchString(value.Name) || !safeIDPattern.MatchString(value.Role) ||
		!safeIDPattern.MatchString(value.Platform) || !safeIDPattern.MatchString(value.Arch) ||
		value.MediaType == "" || len(value.MediaType) > 96 || value.Size <= 0 || value.Size > 1<<30 ||
		!digestPattern.MatchString(value.SHA256) || !safeIDPattern.MatchString(value.Provenance) {
		return errors.New("update artifact identity is invalid")
	}
	return nil
}

func artifactSetDigest(artifacts []ArtifactIdentityV1) string {
	copyArtifacts := append([]ArtifactIdentityV1(nil), artifacts...)
	sort.Slice(copyArtifacts, func(i, j int) bool {
		return copyArtifacts[i].Role+"\x00"+copyArtifacts[i].Name < copyArtifacts[j].Role+"\x00"+copyArtifacts[j].Name
	})
	data, _ := json.Marshal(copyArtifacts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeOperationID(value string) bool {
	return len(value) <= 96 && safeIDPattern.MatchString(value) && strings.HasPrefix(value, "update-operation:")
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value)
}

func semanticRef(kind, operationID, manifestDigest string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + operationID + "\x00" + manifestDigest))
	return hex.EncodeToString(sum[:])
}
