package update

import (
	"context"
	"os"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func TestBrokerProjectionBindsTheCompleteAuthenticatedProviderSource(t *testing.T) {
	fixture := newLifecycleFixture(t)
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		t.Run(id, func(t *testing.T) {
			manifest := fixture.verified.Manifest
			manifest.Schema = release.SchemaV2
			source := providerfixture.Identity(t, id).Source
			manifest.CoreProviders = []release.CoreProviderSource{source}
			manifest.Artifacts = append([]release.Artifact(nil), manifest.Artifacts...)
			for i := range manifest.Artifacts {
				manifest.Artifacts[i].CoreProvider = id
			}
			verified, _, _, err := verifyArtifactFixture(manifest, fixture.manager.now())
			if err != nil {
				t.Fatal(err)
			}
			selected, err := verified.PanelArtifactsForProvider("linux", "amd64", "full", id)
			if err != nil {
				t.Fatal(err)
			}
			request := fixture.prepareRequest("provider-projection-" + id)
			request.ExpectedManifestDigest = verified.Digest
			operation := newUpdateOperation(request, verified, selected, "linux", "amd64", "full", fixture.manager.now())
			identity, err := releaseIdentity(operation, verified, selected)
			if err != nil || contract.ValidateRelease(identity) != nil || identity.CoreProvider != source ||
				identity.Artifacts[0].CoreProvider != id || identity.ArtifactSetDigest != release.ArtifactSetDigest(selected) {
				t.Fatal("broker projection lost authenticated source/artifact binding", err)
			}
			for name, mutate := range map[string]func(*model.UpdateOperation, *release.Verified, []release.Artifact){
				"manifest": func(op *model.UpdateOperation, _ *release.Verified, _ []release.Artifact) {
					op.ManifestDigest = lifecycleDigest("different-manifest")
				},
				"artifact-set": func(op *model.UpdateOperation, _ *release.Verified, _ []release.Artifact) {
					op.ArtifactSetDigest = lifecycleDigest("different-selection")
				},
				"profile": func(op *model.UpdateOperation, _ *release.Verified, _ []release.Artifact) { op.BinaryProfile = "core" },
				"target":  func(op *model.UpdateOperation, _ *release.Verified, _ []release.Artifact) { op.Arch = "arm64" },
				"version": func(op *model.UpdateOperation, _ *release.Verified, _ []release.Artifact) { op.Version = "0.0.0" },
				"provider-reference": func(_ *model.UpdateOperation, _ *release.Verified, artifacts []release.Artifact) {
					artifacts[0].CoreProvider = "unknown"
				},
				"forged-trust": func(_ *model.UpdateOperation, v *release.Verified, _ []release.Artifact) {
					*v = release.Verified{Manifest: v.Manifest, Digest: v.Digest, KeyID: v.KeyID}
				},
				"changed-source": func(_ *model.UpdateOperation, v *release.Verified, _ []release.Artifact) {
					v.Manifest.CoreProviders = append([]release.CoreProviderSource(nil), v.Manifest.CoreProviders...)
					v.Manifest.CoreProviders[0].SourceRevision = lifecycleDigest("changed-source")[:40]
				},
			} {
				t.Run(name, func(t *testing.T) {
					changedOperation, changedVerified := operation, verified
					changedArtifacts := append([]release.Artifact(nil), selected...)
					mutate(&changedOperation, &changedVerified, changedArtifacts)
					if _, err := releaseIdentity(changedOperation, changedVerified, changedArtifacts); err == nil {
						t.Fatal("changed/forged release acquired a broker identity")
					}
				})
			}
		})
	}
}

func TestBrokerSelectedArtifactMismatchIsRejectedBeforeOpeningCache(t *testing.T) {
	fixture := newLifecycleFixture(t)
	selected, err := fixture.verified.PanelArtifacts("linux", "amd64", "full")
	if err != nil {
		t.Fatal(err)
	}
	operation := newUpdateOperation(fixture.prepareRequest("changed-artifact-selection"), fixture.verified, selected,
		"linux", "amd64", "full", fixture.manager.now())
	operation.ArtifactSetDigest = lifecycleDigest("other-selection")
	root := t.TempDir()
	provider := &BrokerProvider{}
	if err := provider.downloadAndStageArtifact(context.Background(), operation, fixture.verified, selected[0], root, nil); err == nil {
		t.Fatal("different selection reached fetch/staging")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected selection touched the artifact cache", err)
	}
}
