package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/database/schema"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func verifyArtifactFixture(manifest release.Manifest, now time.Time) (release.Verified, []byte, release.TrustStore, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return release.Verified{}, nil, release.TrustStore{}, err
	}
	raw, err := release.Sign(manifest, "release-test", private)
	if err != nil {
		return release.Verified{}, nil, release.TrustStore{}, err
	}
	trust, err := release.NewTrustStore([]release.TrustRoot{{KeyID: "release-test", PublicKey: public, State: release.RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		return release.Verified{}, nil, release.TrustStore{}, err
	}
	verified, err := release.Verify(raw, trust, now, release.ChannelMain, 0)
	return verified, raw, trust, err
}

func TestBrokerArtifactTrustRejectsBeforeOpeningCache(t *testing.T) {
	fixture := newLifecycleFixture(t)
	root := t.TempDir()
	artifact := fixture.verified.Manifest.Artifacts[0]
	provider := &BrokerProvider{}
	operation := model.UpdateOperation{ManifestDigest: fixture.verified.Digest, Version: fixture.verified.Manifest.Version,
		Platform: "linux", Arch: "amd64", BinaryProfile: "full"}
	for _, candidate := range []struct {
		verified  release.Verified
		artifact  release.Artifact
		operation model.UpdateOperation
	}{
		{release.Verified{Manifest: fixture.verified.Manifest, Digest: fixture.verified.Digest}, artifact, operation},
		{fixture.verified, fixture.verified.Manifest.Artifacts[1], operation},
		{fixture.verified, artifact, model.UpdateOperation{}},
	} {
		if err := provider.downloadAndStageArtifact(t.Context(), candidate.operation, candidate.verified, candidate.artifact, root, nil); err == nil {
			t.Fatal("unbound artifact reached staging")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("trust rejection touched cache: %d %v", len(entries), err)
	}
}

func TestUpdateAdmissionConsumesPanelAndComponentArtifactTrust(t *testing.T) {
	for _, wrongClass := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed-catalog", true: "wrong-artifact-class"}[wrongClass], func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			manifest := fixture.verified.Manifest
			catalog := release.Artifact{Name: "solovey-ui-components.tar.gz", Role: "component-catalog", Platform: "any", Arch: "any",
				MediaType: "application/gzip", Size: 80, SHA256: lifecycleDigest("catalog"), Provenance: "test"}
			manifest.Artifacts = append(manifest.Artifacts, catalog)
			componentDigest := catalog.SHA256
			if wrongClass {
				componentDigest = manifest.Artifacts[0].SHA256
			}
			manifest.Components = []release.Component{{ID: "telegram", Version: "1.0.0", ArtifactSHA256: componentDigest,
				MinimumCoreSchema: schema.CurrentCoreVersion, MaximumCoreSchema: schema.CurrentCoreVersion}}
			verified, raw, trust, err := verifyArtifactFixture(manifest, fixture.manager.now())
			if err != nil {
				t.Fatal(err)
			}
			fixture.manager.fetcher, fixture.manager.trust = lifecycleReleaseClient{raw: raw}, trust
			check, err := fixture.manager.Check(t.Context(), release.ChannelMain)
			if wrongClass {
				if err == nil {
					t.Fatal("component bound to panel archive accepted")
				}
				var rows int64
				if err := fixture.db.Model(&model.UpdateReleaseState{}).Count(&rows).Error; err != nil || rows != 0 {
					t.Fatalf("failed trust created authority: rows=%d err=%v", rows, err)
				}
				return
			}
			if err != nil || len(check.ArtifactTrust) != 1 || len(check.ComponentTrust) != 1 {
				t.Fatalf("artifact trust consumers: %+v %v", check, err)
			}
			panel, component := check.ArtifactTrust[0], check.ComponentTrust[0]
			if panel.Owner != "panel" || component.Artifact.Owner != "components" || component.Artifact.Kind != "component-catalog" ||
				panel.SourceRevision != verified.Digest || component.Artifact.SignerRef != panel.SignerRef {
				t.Fatalf("trust identity drift: %+v %+v", panel, component)
			}
			request := fixture.prepareRequest("artifact-trust")
			request.ExpectedManifestDigest = verified.Digest
			operation, err := fixture.manager.Prepare(context.Background(), request)
			if err != nil || operation.State != string(StatePrepared) || operation.ManifestDigest != panel.SourceRevision {
				t.Fatalf("signed trust admission: %+v %v", operation, err)
			}
		})
	}
}
