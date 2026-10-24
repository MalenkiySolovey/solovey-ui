//go:build linux

package updatebroker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func writeGenerationIdentityFixture(t *testing.T, host *Host, name string, identity ReleaseIdentityV1) {
	t.Helper()
	root := filepath.Join(host.ReleaseRoot, name)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-identity.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProviderGenerationsInOneManifestDoNotCollideAndRecoverWithoutInventedSource(t *testing.T) {
	host := NewHost()
	host.ReleaseRoot = t.TempDir()
	official, extended := providerReleaseFixture(t, "sing-box"), providerReleaseFixture(t, "shtorm-extended")
	if official.Sequence != extended.Sequence || official.ManifestDigest != extended.ManifestDigest ||
		releaseDirectoryName(official) == releaseDirectoryName(extended) || !releaseDirectoryPattern.MatchString(releaseDirectoryName(extended)) {
		t.Fatal("provider selections shared a generation or lost the closed namespace")
	}
	for _, expected := range []ReleaseIdentityV1{official, extended} {
		name := releaseDirectoryName(expected)
		if !stagingDirectoryPattern.MatchString(name + ".staging") {
			t.Fatal("provider generation lost crash-recovery staging ownership")
		}
		writeGenerationIdentityFixture(t, host, name, expected)
		request := expected
		request.CoreProvider, request.Artifacts = release.CoreProviderSource{}, nil
		resolved, stored, err := host.resolveReleaseGeneration(request)
		if err != nil || resolved != name || !sameReleaseIdentity(stored, expected, true) {
			t.Fatal("control identity did not resolve its exact stored provider", err)
		}
	}
	request := official
	request.Artifacts = nil
	request.ArtifactSetDigest = extended.ArtifactSetDigest
	if _, _, err := host.resolveReleaseGeneration(request); err == nil {
		t.Fatal("conflicting source claim resolved another provider's generation")
	}
}

func TestLegacyGenerationLabelsRemainUsableAndAmbiguousAliasesFailClosed(t *testing.T) {
	host := NewHost()
	host.ReleaseRoot = t.TempDir()
	identity := providerReleaseFixture(t, "sing-box")
	identity.CoreProvider, identity.Artifacts[0].CoreProvider = release.CoreProviderSource{}, ""
	identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
	legacy := releaseDirectoryName(identity)
	if !releaseDirectoryPattern.MatchString(legacy) || legacy == providerReleaseDirectoryName(identity) {
		t.Fatal("legacy generation label changed")
	}
	writeGenerationIdentityFixture(t, host, legacy, identity)
	if name, _, err := host.resolveReleaseGeneration(identity); err != nil || name != legacy {
		t.Fatal("legacy prepared generation was not recovered", err)
	}
	writeGenerationIdentityFixture(t, host, providerReleaseDirectoryName(identity), identity)
	if _, _, err := host.resolveReleaseGeneration(identity); err == nil {
		t.Fatal("two generation objects acquired one unambiguous activation identity")
	}
}
