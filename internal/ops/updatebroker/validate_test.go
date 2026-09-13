package updatebroker

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestReleaseIdentityRecomputesTheSignedArtifactSetDigest(t *testing.T) {
	artifact := release.Artifact{Name: "solovey-ui-linux-amd64.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64",
		MediaType: "application/gzip", Size: 101, SHA256: updateDigest("artifact"), Provenance: "release-ci"}
	identityArtifact := ArtifactIdentityV1{Name: artifact.Name, Role: artifact.Role, Platform: artifact.Platform, Arch: artifact.Arch,
		MediaType: artifact.MediaType, Size: artifact.Size, SHA256: artifact.SHA256, Provenance: artifact.Provenance}
	identity := ReleaseIdentityV1{ReleaseID: "solovey-ui-main-9", Sequence: 9, Version: "2026.3.0", ManifestDigest: updateDigest("manifest"),
		ArtifactSetDigest: release.ArtifactSetDigest([]release.Artifact{artifact}), BinaryProfile: "full",
		DeploymentRevision: updateDigest("deployment"), MigrationSetDigest: updateDigest("migration"),
		RestartClass: "stack", RollbackClass: "automatic", Artifacts: []ArtifactIdentityV1{identityArtifact}}
	if err := ValidateRelease(identity); err != nil {
		t.Fatalf("matching signed artifact projection was rejected: %v", err)
	}
	identity.Artifacts[0].Size++
	if err := ValidateRelease(identity); err == nil {
		t.Fatal("artifact metadata drift was accepted against the signed set digest")
	}
	identity.Artifacts[0] = identityArtifact
	identity.Artifacts[0].Role = "panel-core"
	if err := ValidateRelease(identity); err == nil {
		t.Fatal("cross-profile artifact was accepted")
	}
}

func TestOperationIdentityAndSemanticReferencesAreClosedAndStable(t *testing.T) {
	const operationID = "update-operation:release-9"
	if !safeOperationID(operationID) {
		t.Fatal("valid typed operation ID was rejected")
	}
	for _, invalid := range []string{
		"release-9",
		"update-operation:release/9",
		"update-operation:" + strings.Repeat("a", 97),
	} {
		if safeOperationID(invalid) {
			t.Fatalf("unsafe operation ID was accepted: %q", invalid)
		}
	}

	manifest := updateDigest("manifest")
	if !validDigest(manifest) || validDigest(strings.ToUpper(manifest)) || validDigest("short") {
		t.Fatal("digest validation is not closed over lowercase SHA-256")
	}
	first := semanticRef("stage", operationID, manifest)
	if !validDigest(first) || first != semanticRef("stage", operationID, manifest) {
		t.Fatal("semantic reference is not a stable SHA-256 identity")
	}
	if first == semanticRef("apply", operationID, manifest) ||
		first == semanticRef("stage", operationID, updateDigest("other-manifest")) {
		t.Fatal("semantic reference did not separate operation semantics")
	}
}

func TestStagingChunkAdmissionIsPortableOverflowSafeAndExact(t *testing.T) {
	for _, test := range []struct {
		name       string
		offset     int64
		chunkBytes int
		size       int64
		final      bool
		want       int64
		valid      bool
	}{
		{"below-boundary", 0, 3, 4, false, 3, true},
		{"exact-final-boundary", 3, 1, 4, true, 4, true},
		{"one-above-boundary", 3, 2, 4, false, 0, false},
		{"repeated-non-final-near-limit", 1<<30 - MaxChunkBytes - 1, MaxChunkBytes, 1 << 30, false, 1<<30 - 1, true},
		{"near-limit-final", 1<<30 - 1, 1, 1 << 30, true, 1 << 30, true},
		{"invalid-negative-offset", -1, 1, 4, false, 0, false},
		{"overflow-shaped-offset", math.MaxInt64 - 1, MaxChunkBytes, math.MaxInt64, false, 0, false},
		{"premature-final", 0, 1, 4, true, 0, false},
		{"missing-final-at-boundary", 3, 1, 4, false, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateChunkAdmission(test.offset, test.chunkBytes, test.size, test.final)
			if (err == nil) != test.valid || got != test.want {
				t.Fatalf("accepted=%d err=%v want=%d valid=%v", got, err, test.want, test.valid)
			}
		})
	}
}

func updateDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
