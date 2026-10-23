package updatebroker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func providerReleaseFixture(t *testing.T, id string) ReleaseIdentityV1 {
	t.Helper()
	artifact := ArtifactIdentityV1{Name: "provider-fixture.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64",
		MediaType: "application/gzip", Size: 101, SHA256: updateDigest("provider-content"), Provenance: "fixture", CoreProvider: id}
	return ReleaseIdentityV1{ReleaseID: "fixture-provider-release", Sequence: 19, Version: "2026.4.1",
		ManifestDigest: updateDigest("provider-manifest"), ArtifactSetDigest: artifactSetDigest([]ArtifactIdentityV1{artifact}),
		BinaryProfile: "full", DeploymentRevision: updateDigest("provider-deployment"), MigrationSetDigest: updateDigest("provider-migrations"),
		RestartClass: "stack", RollbackClass: "automatic", Artifacts: []ArtifactIdentityV1{artifact}, CoreProvider: providerfixture.Identity(t, id).Source}
}

func TestBrokerProviderIdentityRetainsTheSignedArtifactCanonicalShape(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		identity := providerReleaseFixture(t, id)
		artifact := identity.Artifacts[0]
		signed := release.Artifact{Name: artifact.Name, Role: artifact.Role, Platform: artifact.Platform, Arch: artifact.Arch,
			MediaType: artifact.MediaType, Size: artifact.Size, SHA256: artifact.SHA256, Provenance: artifact.Provenance, CoreProvider: id}
		if ValidateRelease(identity) != nil || identity.ArtifactSetDigest != release.ArtifactSetDigest([]release.Artifact{signed}) {
			t.Fatal("typed broker lost signed provider/artifact-set identity")
		}
		raw, err := json.Marshal(identity)
		if err != nil {
			t.Fatal(err)
		}
		var reopened ReleaseIdentityV1
		if json.Unmarshal(raw, &reopened) != nil || ValidateRelease(reopened) != nil || reopened.CoreProvider != identity.CoreProvider || reopened.Artifacts[0] != artifact {
			t.Fatal("provider source changed in a durable identity round trip")
		}
	}
	legacy, err := json.Marshal(ReleaseIdentityV1{})
	if err != nil || string(legacy) != `{"releaseId":"","sequence":0,"version":"","manifestDigest":"","artifactSetDigest":"","binaryProfile":"","deploymentRevision":"","migrationSetDigest":"","restartClass":"","rollbackClass":"","artifacts":null}` {
		t.Fatal("legacy broker identity bytes changed")
	}
}

func TestBrokerProviderIdentityRejectsIncompleteConflictingAndUnknownSources(t *testing.T) {
	for name, mutate := range map[string]func(*ReleaseIdentityV1){
		"missing-source":             func(v *ReleaseIdentityV1) { v.CoreProvider = release.CoreProviderSource{} },
		"missing-provider-reference": func(v *ReleaseIdentityV1) { v.Artifacts[0].CoreProvider = "" },
		"other-provider-reference":   func(v *ReleaseIdentityV1) { v.Artifacts[0].CoreProvider = "sing-box" },
		"unknown-provider":           func(v *ReleaseIdentityV1) { v.Artifacts[0].CoreProvider = "credential-sentinel" },
		"invalid-source-checksum":    func(v *ReleaseIdentityV1) { v.CoreProvider.ModuleSum = "credential-sentinel" },
		"invalid-source-repository":  func(v *ReleaseIdentityV1) { v.CoreProvider.SourceRepository = "https://credential-sentinel.invalid" },
		"invalid-source-commit":      func(v *ReleaseIdentityV1) { v.CoreProvider.SourceRevision = "credential-sentinel" },
	} {
		t.Run(name, func(t *testing.T) {
			identity := providerReleaseFixture(t, "shtorm-extended")
			mutate(&identity)
			identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
			err := ValidateRelease(identity)
			if err == nil || strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatal("incomplete/conflicting provider acquired a valid identity or echoed input")
			}
		})
	}
}
