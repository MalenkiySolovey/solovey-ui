//go:build linux

package updatebroker

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestPreparedProviderIdentityComparisonDoesNotStripSourceClaims(t *testing.T) {
	stored := providerReleaseFixture(t, "shtorm-extended")
	if !sameReleaseIdentity(stored, stored, true) {
		t.Fatal("exact provider identity did not match")
	}
	changed := stored
	changed.CoreProvider.SourceRevision = updateDigest("different-source")[:40]
	if sameReleaseIdentity(stored, changed, true) || sameReleaseIdentity(stored, changed, false) {
		t.Fatal("changed source claim matched a prepared generation")
	}
	control := stored
	control.Artifacts, control.CoreProvider = nil, release.CoreProviderSource{}
	if !sameReleaseIdentity(stored, control, false) || sameReleaseIdentity(stored, control, true) {
		t.Fatal("bound control projection was confused with complete generation metadata")
	}
	if verifyPreparedProvider(t.TempDir(), stored) == nil {
		t.Fatal("provider declaration authorized a missing executable")
	}
}

func TestPreparedProviderActualExecutable(t *testing.T) {
	binary := os.Getenv("SOLOVEY_PROVIDER_TEST_BINARY")
	if binary == "" {
		t.Skip("actual executable source probe is supplied during provider qualification")
	}
	if os.Geteuid() != 0 {
		t.Fatal("explicit provider object probe requires root ownership")
	}
	id, profile := os.Getenv("SOLOVEY_PROVIDER_TEST_ID"), os.Getenv("SOLOVEY_PROVIDER_TEST_PROFILE")
	if (id != "sing-box" && id != "shtorm-extended") || (profile != "full" && profile != "core") {
		t.Fatal("actual provider object probe requires an explicit flavor/profile")
	}
	identity := providerReleaseFixture(t, id)
	identity.BinaryProfile, identity.Artifacts[0].Role = profile, "panel-"+profile
	identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
	root := t.TempDir()
	panel := filepath.Join(root, "solovey-ui")
	input, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(panel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(panel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyPreparedProvider(root, identity); err != nil {
		t.Fatal("actual provider object did not match", err)
	}
	digest, err := fileDigest(panel)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("provider=%s profile=%s binary_sha256=%s source_revision=%s", id, profile, digest, identity.CoreProvider.SourceRevision)
	for name, change := range map[string]func(*ReleaseIdentityV1){
		"other-provider": func(v *ReleaseIdentityV1) {
			other := "sing-box"
			if id == other {
				other = "shtorm-extended"
			}
			v.CoreProvider = providerfixture.Identity(t, other).Source
			v.Artifacts[0].CoreProvider = other
		},
		"other-profile": func(v *ReleaseIdentityV1) {
			v.BinaryProfile = "full"
			if profile == v.BinaryProfile {
				v.BinaryProfile = "core"
			}
			v.Artifacts[0].Role = "panel-" + v.BinaryProfile
		},
		"other-machine": func(v *ReleaseIdentityV1) { v.Artifacts[0].Arch = "arm64" },
		"other-module-sum": func(v *ReleaseIdentityV1) {
			v.CoreProvider.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := identity
			changed.Artifacts = append([]ArtifactIdentityV1(nil), identity.Artifacts...)
			change(&changed)
			changed.ArtifactSetDigest = artifactSetDigest(changed.Artifacts)
			if ValidateRelease(changed) != nil || verifyPreparedProvider(root, changed) == nil {
				t.Fatal("independently observed executable accepted a conflicting declaration")
			}
		})
	}
	if err := os.Chmod(panel, 0o777); err != nil {
		t.Fatal(err)
	}
	if verifyPreparedProvider(root, identity) == nil {
		t.Fatal("provider metadata bypassed executable-object ownership/mode policy")
	}
}
