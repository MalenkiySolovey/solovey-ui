package update

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

// Release-consumer reproducer only: unchanged canonical signed v2 fixture,
// actual current-provider owner and ordinary Update.Check. No network or host effects.
func TestR7ReleaseV2CheckKeepsInstalledProvider(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		t.Run(id, func(t *testing.T) {
			fixture, _ := providerTransitionFixture(t)
			fixture.manager.coreProvider = updateTestCoreProvider(t, id)
			result, err := fixture.manager.Check(t.Context(), release.ChannelMain)
			if err != nil || result.SigningStatus != "VERIFIED" {
				t.Fatalf("ordinary Update.Check cannot resolve its valid signed v2 set for installed %s: signing=%s reasons=%v err=%v", id, result.SigningStatus, result.ReasonCodes, err)
			}
			if len(result.ArtifactTrust) != 1 || result.ArtifactTrust[0].CoreProvider.ID != id {
				t.Fatal("ordinary check failed to retain the installed provider identity")
			}
		})
	}
}

func TestOrdinaryV2DiscoveryBindsSignedFutureSourceWithoutChangingProvider(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		t.Run(id, func(t *testing.T) {
			fixture, _ := providerTransitionFixture(t)
			fixture.manager.coreProvider = updateTestCoreProvider(t, id)
			manifest := fixture.verified.Manifest
			manifest.CoreProviders = append([]release.CoreProviderSource(nil), manifest.CoreProviders...)
			var target release.CoreProviderSource
			for i := range manifest.CoreProviders {
				if manifest.CoreProviders[i].ID == id {
					manifest.CoreProviders[i].SourceRevision = lifecycleDigest("future-qualified-source")[:40]
					manifest.CoreProviders[i].ProviderVersion = "v0.0.0-20261108000000-" + manifest.CoreProviders[i].SourceRevision[:12]
					manifest.CoreProviders[i].ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
					target = manifest.CoreProviders[i]
				}
			}
			_, raw, trust, err := verifyArtifactFixture(manifest, fixture.manager.now())
			if err != nil {
				t.Fatal(err)
			}
			fixture.manager.fetcher, fixture.manager.trust = lifecycleReleaseClient{raw: raw}, trust
			checked, err := fixture.manager.Check(t.Context(), release.ChannelMain)
			if err != nil || len(checked.ArtifactTrust) != 1 || checked.ArtifactTrust[0].CoreProvider != target {
				t.Fatal("signed future source was confused with a provider transition", err)
			}
			if fixture.provider.stageCalls != 0 || fixture.provider.activateCalls != 0 {
				t.Fatal("discovery granted installation authority")
			}
		})
	}
}

func TestOrdinaryV2PrepareActivateRollbackRetainsProvider(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		for _, profile := range []string{"full", "core"} {
			t.Run(id+"/"+profile, func(t *testing.T) {
				fixture, owner := providerTransitionFixture(t)
				fixture.manager.coreProvider = updateTestCoreProvider(t, id)
				fixture.manager.profile = func() string { return profile }
				checked, err := fixture.manager.Check(t.Context(), release.ChannelMain)
				if err != nil || len(checked.ArtifactTrust) != 1 || checked.ArtifactTrust[0].CoreProvider.ID != id {
					t.Fatal("ordinary check lost its provider", err)
				}
				prepared, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("ordinary-v2"))
				if err != nil || State(prepared.State) != StatePrepared || prepared.BinaryProfile != profile ||
					fixture.provider.stagedSource != fixture.manager.coreProvider().Source {
					t.Fatal("ordinary prepare lost the selected source/profile", err)
				}
				applied, err := fixture.manager.Activate(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
				if err != nil || State(applied.State) != StateApplied || fixture.provider.activateCalls != 1 {
					t.Fatal("ordinary activation failed", err)
				}
				rolled, err := fixture.manager.Rollback(t.Context(), RevisionRequest{OperationID: applied.OperationID, ExpectedRevision: applied.Revision})
				if err != nil || State(rolled.State) != StateRolledBack || fixture.provider.rollbackCalls != 1 {
					t.Fatal("ordinary rollback failed", err)
				}
				if owner.captures != 0 || owner.mutations != 0 {
					t.Fatal("ordinary update acquired provider-maintenance authority")
				}
			})
		}
	}
}

func TestOrdinaryV2ProviderChangeAfterPrepareFailsBeforeActivation(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		t.Run(id, func(t *testing.T) {
			fixture, _ := providerTransitionFixture(t)
			fixture.manager.coreProvider = updateTestCoreProvider(t, id)
			prepared, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("changed-provider"))
			if err != nil {
				t.Fatal(err)
			}
			other := "sing-box"
			if id == other {
				other = "shtorm-extended"
			}
			fixture.manager.coreProvider = updateTestCoreProvider(t, other)
			if _, err := fixture.manager.Activate(t.Context(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision}); !errors.Is(err, ErrCoreProviderMaintenance) {
				t.Fatal("provider change was activated as an ordinary update", err)
			}
			var stored model.UpdateOperation
			if err := fixture.db.First(&stored, "operation_id = ?", prepared.OperationID).Error; err != nil || stored.State != prepared.State || stored.Revision != prepared.Revision || fixture.provider.activateCalls != 0 {
				t.Fatal("denied activation changed durable authority", err)
			}
		})
	}
}

func TestOrdinaryV2RejectsUntrustedOrMismatchedTargetsBeforeEffects(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		for _, control := range []string{"missing-provider", "wrong-provenance", "wrong-version-range", "wrong-arch", "wrong-profile", "tampered-signature", "expired", "stale-sequence", "unknown-schema"} {
			t.Run(id+"/"+control, func(t *testing.T) {
				fixture, owner := providerTransitionFixture(t)
				fixture.manager.coreProvider = updateTestCoreProvider(t, id)
				manifest := fixture.verified.Manifest
				manifest.Artifacts = append([]release.Artifact(nil), manifest.Artifacts...)
				manifest.CoreProviders = append([]release.CoreProviderSource(nil), manifest.CoreProviders...)
				switch control {
				case "missing-provider", "wrong-arch":
					filtered := manifest.Artifacts[:0]
					for _, a := range manifest.Artifacts {
						if a.CoreProvider == id {
							if control == "missing-provider" {
								continue
							}
							a.Arch = "arm64"
						}
						filtered = append(filtered, a)
					}
					manifest.Artifacts = filtered
					if control == "missing-provider" {
						for i, source := range manifest.CoreProviders {
							if source.ID == id {
								manifest.CoreProviders = append(manifest.CoreProviders[:i], manifest.CoreProviders[i+1:]...)
								break
							}
						}
					}
				case "wrong-profile":
					fixture.manager.profile = func() string { return "custom" }
				case "wrong-provenance":
					manifest.Artifacts[0].Provenance = "wrong-source"
				case "wrong-version-range":
					manifest.MinimumPanelVersion = manifest.Version
				case "expired":
					manifest.IssuedAt, manifest.ExpiresAt = fixture.manager.now().Add(-2*time.Hour).Unix(), fixture.manager.now().Add(-time.Hour).Unix()
				case "stale-sequence":
					if err := fixture.manager.repo.saveVerified(t.Context(), fixture.verified, release.ChannelMain, true, fixture.manager.now().Unix()); err != nil {
						t.Fatal(err)
					}
					if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", release.ChannelMain).Update("last_applied_sequence", manifest.Sequence).Error; err != nil {
						t.Fatal(err)
					}
				}
				_, raw, trust, _ := verifyArtifactFixture(manifest, fixture.manager.now())
				if len(raw) == 0 {
					t.Fatal("negative control did not produce signed bytes")
				}
				if control == "tampered-signature" {
					raw = bytes.Replace(raw, []byte(`"sequence":17`), []byte(`"sequence":18`), 1)
				}
				if control == "unknown-schema" {
					raw = bytes.Replace(raw, []byte(release.SchemaV2), []byte("solovey.release/v99"), 1)
				}
				fixture.manager.fetcher, fixture.manager.trust = lifecycleReleaseClient{raw: raw}, trust
				checked, err := fixture.manager.Check(t.Context(), release.ChannelMain)
				if err == nil || len(checked.ArtifactTrust) != 0 {
					t.Fatal("invalid ordinary check acquired artifact trust")
				}
				if _, err := fixture.manager.Prepare(t.Context(), fixture.prepareRequest("negative-v2")); err == nil {
					t.Fatal("invalid target was prepared")
				}
				var operations int64
				if err := fixture.db.Model(&model.UpdateOperation{}).Count(&operations).Error; err != nil || operations != 0 || fixture.provider.stageCalls != 0 || fixture.provider.activateCalls != 0 || owner.mutations != 0 || owner.captures != 0 {
					t.Fatal("negative control reached host or maintenance effects", err)
				}
			})
		}
	}
}
