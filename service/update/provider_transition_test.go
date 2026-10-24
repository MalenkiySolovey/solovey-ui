package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

type transitionConfigurationFixture struct {
	captures, mutations int
	last                []byte
}

func (owner *transitionConfigurationFixture) CaptureProviderConfiguration(context.Context) (CoreConfigurationSnapshot, error) {
	owner.captures++
	owner.last = []byte(`{"credential":"transition-preview-secret-sentinel"}`)
	return CoreConfigurationSnapshot{Configuration: owner.last,
		ConfigurationDigest: lifecycleDigest(string(owner.last)), ConfigurationRevision: lifecycleDigest("desired-config"),
		CapabilityRevision: lifecycleDigest("capabilities"), RuntimeState: "STOPPED",
		Preserved: []PreservedConfiguration{{Category: "endpoints", ID: 7, Type: "preserved", Reason: "UNAVAILABLE"}}}, nil
}

func (owner *transitionConfigurationFixture) SetCoreMaintenance(context.Context, string, bool) error {
	owner.mutations++
	return nil
}

func providerTransitionFixture(t *testing.T) (lifecycleFixture, *transitionConfigurationFixture) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	manifest := fixture.verified.Manifest
	manifest.Schema = release.SchemaV2
	manifest.Artifacts = nil
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		manifest.CoreProviders = append(manifest.CoreProviders, updateTestCoreProvider(t, id)().Source)
		for _, artifact := range fixture.verified.Manifest.Artifacts {
			artifact.CoreProvider = id
			artifact.Name = id + "-" + artifact.Name
			artifact.SHA256 = lifecycleDigest(id + artifact.Name)
			manifest.Artifacts = append(manifest.Artifacts, artifact)
		}
	}
	verified, raw, trust, err := verifyArtifactFixture(manifest, fixture.manager.now())
	if err != nil {
		t.Fatal(err)
	}
	fixture.verified, fixture.manager.fetcher, fixture.manager.trust = verified, lifecycleReleaseClient{raw: raw}, trust
	owner := &transitionConfigurationFixture{}
	fixture.manager.SetCoreConfigurationOwner(owner)
	return fixture, owner
}

func TestProviderTransitionPreviewBindsExplicitSelectionWithoutMutationOrSecretProjection(t *testing.T) {
	for _, target := range []string{"sing-box", "shtorm-extended"} {
		t.Run(target, func(t *testing.T) {
			fixture, owner := providerTransitionFixture(t)
			preview, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, target)
			if err != nil || preview.State != "TARGET_VALIDATION_REQUIRED" || preview.Target.ID != target || preview.ArtifactTrust.CoreProvider != preview.Target {
				t.Fatal("explicit source did not retain signed target identity", err)
			}
			selected, err := fixture.verified.PanelArtifactsForProvider("linux", "amd64", "full", target)
			if err != nil || preview.ArtifactSetDigest != release.ArtifactSetDigest(selected) || preview.ManifestDigest != fixture.verified.Digest {
				t.Fatal("preview lost selected artifact binding", err)
			}
			encoded, err := json.Marshal(preview)
			if err != nil || bytes.Contains(encoded, []byte("secret-sentinel")) || len(preview.Configuration.Configuration) != 0 || bytes.Count(owner.last, []byte{0}) != len(owner.last) {
				t.Fatal("read projection retained raw configuration")
			}
			if owner.captures != 1 || owner.mutations != 0 || fixture.provider.stageCalls != 0 || fixture.provider.activateCalls != 0 || fixture.provider.rollbackCalls != 0 {
				t.Fatal("preview performed a maintenance or installation effect")
			}
			for _, row := range []any{&model.UpdateOperation{}, &model.UpdateReleaseState{}, &model.UpdateJournal{}} {
				var count int64
				if err := fixture.db.Model(row).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("preview published durable update authority", err)
				}
			}
		})
	}
}

func TestProviderTransitionPreviewEqualSequenceRequiresAppliedExactEnvelopeAndDifferentProvider(t *testing.T) {
	fixture, _ := providerTransitionFixture(t)
	selected, err := fixture.verified.PanelArtifactsForProvider("linux", "amd64", "full", "sing-box")
	if err != nil {
		t.Fatal(err)
	}
	op := newUpdateOperation(fixture.prepareRequest("applied-provider-envelope"), fixture.verified, selected, "linux", "amd64", "full", fixture.manager.now())
	op.State, op.BytesCompleted = string(StateApplied), op.BytesTotal
	if err := fixture.manager.repo.saveVerified(t.Context(), fixture.verified, release.ChannelMain, true, fixture.manager.now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", release.ChannelMain).Update("last_applied_sequence", op.Sequence).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, "shtorm-extended"); err != nil {
		t.Fatal("exact same-envelope flavor change rejected", err)
	}
	if _, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, "sing-box"); !errors.Is(err, ErrReleaseChanged) {
		t.Fatal("same-envelope same-provider replay was offered", err)
	}
	manifest := fixture.verified.Manifest
	manifest.Artifacts = append([]release.Artifact(nil), manifest.Artifacts...)
	manifest.Artifacts[0].SHA256 = lifecycleDigest("changed-envelope")
	_, raw, trust, err := verifyArtifactFixture(manifest, fixture.manager.now())
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager.fetcher, fixture.manager.trust = lifecycleReleaseClient{raw: raw}, trust
	if _, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, "shtorm-extended"); !errors.Is(err, ErrReleaseChanged) {
		t.Fatal("different envelope reused the applied sequence", err)
	}
	if err := fixture.db.Delete(&op).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.repo.appliedManifest(t.Context(), release.ChannelMain, op.Sequence); err == nil {
		t.Fatal("missing applied authority was inferred from a latest-observed row")
	}
}

func TestProviderTransitionPreviewUnknownSourceAndTargetFailBeforeFetch(t *testing.T) {
	fixture, owner := providerTransitionFixture(t)
	client := &providerGuardReleaseClient{ReleaseClient: fixture.manager.fetcher}
	fixture.manager.fetcher = client
	if _, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, "future-unknown"); err == nil {
		t.Fatal("unknown target accepted")
	}
	fixture.manager.coreProvider = func() coreprovider.Identity { return coreprovider.Resolve(nil) }
	if _, err := fixture.manager.PreviewProviderTransition(t.Context(), release.ChannelMain, "sing-box"); !errors.Is(err, ErrCoreProviderUnverified) {
		t.Fatal("unknown installed source accepted", err)
	}
	if client.calls != 0 || owner.captures != 0 || owner.mutations != 0 {
		t.Fatal("unknown source/target reached transport or configuration")
	}
}
