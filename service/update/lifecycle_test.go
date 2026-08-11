package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type lifecycleReleaseClient struct{ raw []byte }

func (client lifecycleReleaseClient) Fetch(context.Context, release.Source) ([]byte, error) {
	return append([]byte(nil), client.raw...), nil
}

type lifecycleProvider struct {
	downloadErr        error
	preflightAvailable bool
	preflightErr       error
	activateErr        error
	verifyActive       bool
	verifyErr          error
	rollbackVerified   bool
	rollbackErr        error
	reconcileState     State
	reconcileErr       error
}

func (provider *lifecycleProvider) Capabilities(context.Context) Capabilities {
	return Capabilities{Mode: "native", Check: "AVAILABLE", Download: "AVAILABLE", Prepare: "AVAILABLE",
		Activate: "AVAILABLE", Rollback: "AVAILABLE", OSUpdates: "EXTERNAL_MANAGED", Reboot: "OPERATOR_ADVISORY"}
}
func (provider *lifecycleProvider) DownloadAndStage(_ context.Context, _ model.UpdateOperation, _ release.Verified, artifacts []release.Artifact, progress func(int64)) error {
	if provider.downloadErr == nil {
		for _, artifact := range artifacts {
			progress(artifact.Size)
		}
	}
	return provider.downloadErr
}
func (provider *lifecycleProvider) Preflight(context.Context, model.UpdateOperation, release.Verified, []release.Artifact) (PreflightResult, error) {
	return PreflightResult{RollbackAvailable: provider.preflightAvailable, BackupRef: lifecycleDigest("fixture-backup")}, provider.preflightErr
}
func (provider *lifecycleProvider) Activate(context.Context, model.UpdateOperation) error {
	return provider.activateErr
}
func (provider *lifecycleProvider) VerifyActive(context.Context, model.UpdateOperation) (bool, error) {
	return provider.verifyActive, provider.verifyErr
}
func (provider *lifecycleProvider) Rollback(context.Context, model.UpdateOperation) (bool, error) {
	return provider.rollbackVerified, provider.rollbackErr
}
func (provider *lifecycleProvider) Reconcile(context.Context, model.UpdateOperation) (State, error) {
	return provider.reconcileState, provider.reconcileErr
}

type lifecycleFixture struct {
	db       *gorm.DB
	manager  *LifecycleManager
	verified release.Verified
	provider *lifecycleProvider
}

func newLifecycleFixture(t testing.TB) lifecycleFixture {
	t.Helper()
	now := time.Unix(1_900_000_000, 0).UTC()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := release.Manifest{Schema: release.SchemaV1, ReleaseID: "solovey-ui-main-17", Sequence: 17, Version: "2026.4.0", Channel: release.ChannelMain,
		IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), DeploymentRevision: lifecycleDigest("deployment"),
		MinimumPanelVersion: "2026.2.0", MaximumPanelVersion: "2026.4.0",
		MinimumCoreSchema: "1.11", MaximumCoreSchema: "1.11", TargetCoreSchema: "1.11", BrokerCapability: "broker-capabilities-1.2",
		MigrationSetDigest: lifecycleDigest("migration"), ReleaseNotesDigest: lifecycleDigest("notes"),
		RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic",
		Artifacts: []release.Artifact{
			{Name: "solovey-ui-linux-amd64.tar.gz", Role: "panel-full", Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 101, SHA256: lifecycleDigest("full"), Provenance: "test"},
			{Name: "solovey-ui-core-linux-amd64.tar.gz", Role: "panel-core", Platform: "linux", Arch: "amd64", MediaType: "application/gzip", Size: 79, SHA256: lifecycleDigest("core"), Provenance: "test"},
		}}
	raw, err := release.Sign(manifest, "release-test", private)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := release.NewTrustStore([]release.TrustRoot{{KeyID: "release-test", PublicKey: public, State: release.RootActive,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), MinSequence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := release.Verify(raw, trust, now, release.ChannelMain, 0)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "update-lifecycle.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.UpdateReleaseState{}, &model.UpdateOperation{}, &model.UpdateJournal{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(t.TempDir(), "absent-installed.json"))
	provider := &lifecycleProvider{preflightAvailable: true, verifyActive: true, rollbackVerified: true, reconcileState: StateApplied}
	manager := NewLifecycleManager(Repository{DB: func() *gorm.DB { return db }}, provider, lifecycleReleaseClient{raw: raw},
		release.Source{ID: "test", Origin: "https://example.test", ManifestPath: "/release.json", ExpectedProvenance: "test"}, trust)
	manager.now = func() time.Time { return now }
	manager.platform = func() (string, string) { return "linux", "amd64" }
	manager.profile = func() string { return "full" }
	manager.health = func(context.Context, model.UpdateOperation) HealthResult {
		return HealthResult{Ready: true, ReasonCodes: []string{"fixture_healthy"}, Revision: lifecycleDigest("fixture-health")}
	}
	return lifecycleFixture{db: db, manager: manager, verified: verified, provider: provider}
}

func BenchmarkUpdatePreview(b *testing.B) {
	fixture := newLifecycleFixture(b)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		status, err := fixture.manager.Check(context.Background(), release.ChannelMain)
		if err != nil || status.Sequence != fixture.verified.Manifest.Sequence {
			b.Fatalf("preview=%#v err=%v", status, err)
		}
	}
}

func BenchmarkUpdateReplayRecoveryProjection(b *testing.B) {
	fixture := newLifecycleFixture(b)
	operation, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("benchmark-replay"))
	if err != nil {
		b.Fatal(err)
	}
	operations := make([]model.UpdateOperation, 200)
	for index := range operations {
		operations[index] = operation
		operations[index].Revision += uint64(index)
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		for _, item := range operations {
			if !validPersistedOperation(item) {
				b.Fatal("valid replay fixture was rejected")
			}
			_ = operationRecoveryProjection(item)
		}
	}
}

func TestPreparePersistsVerifiedIdentityAndAppliedTransitionIsAtomic(t *testing.T) {
	fixture := newLifecycleFixture(t)
	request := PrepareRequest{Channel: release.ChannelMain, ExpectedSequence: fixture.verified.Manifest.Sequence,
		ExpectedManifestDigest: fixture.verified.Digest, IdempotencyKey: "direct-prepare", Acknowledged: true}
	prepared, err := fixture.manager.Prepare(context.Background(), request)
	if err != nil || State(prepared.State) != StatePrepared || !prepared.RollbackAvailable {
		t.Fatalf("prepare=%#v err=%v", prepared, err)
	}
	var observed model.UpdateReleaseState
	if err := fixture.db.First(&observed, "channel = ?", string(release.ChannelMain)).Error; err != nil {
		t.Fatal(err)
	}
	if observed.LastVerifiedSequence != request.ExpectedSequence || observed.LastAppliedSequence != 0 ||
		observed.ReleaseID != fixture.verified.Manifest.ReleaseID || observed.ManifestDigest != request.ExpectedManifestDigest {
		t.Fatalf("direct prepare did not persist verified release identity: %#v", observed)
	}
	applied, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
	if err != nil || State(applied.State) != StateApplied {
		t.Fatalf("activate=%#v err=%v", applied, err)
	}
	if err := fixture.db.First(&observed, "channel = ?", string(release.ChannelMain)).Error; err != nil {
		t.Fatal(err)
	}
	if observed.LastAppliedSequence != request.ExpectedSequence {
		t.Fatalf("applied sequence=%d want=%d", observed.LastAppliedSequence, request.ExpectedSequence)
	}
	var appliedEvents int64
	if err := fixture.db.Model(&model.UpdateJournal{}).Where("operation_id = ? AND state = ? AND event = ?", applied.OperationID, string(StateApplied), "update_applied").Count(&appliedEvents).Error; err != nil || appliedEvents != 1 {
		t.Fatalf("applied journal count=%d err=%v", appliedEvents, err)
	}
	replayed, err := fixture.manager.Prepare(context.Background(), request)
	if err != nil || replayed.OperationID != applied.OperationID || replayed.Revision != applied.Revision {
		t.Fatalf("idempotent replay=%#v err=%v", replayed, err)
	}
}

func TestAppliedOperationRemainsVisibleAndAllowsExactlyOneRollback(t *testing.T) {
	fixture := newLifecycleFixture(t)
	prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("one-rollback"))
	if err != nil {
		t.Fatal(err)
	}
	applied, err := fixture.manager.Activate(context.Background(), RevisionRequest{
		OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
	if err != nil || State(applied.State) != StateApplied {
		t.Fatalf("activate=%#v err=%v", applied, err)
	}
	status := fixture.manager.Status(context.Background(), release.ChannelMain)
	if status.Operation == nil || status.Operation.OperationID != applied.OperationID ||
		State(status.Operation.State) != StateApplied || !status.Operation.RollbackAvailable {
		t.Fatalf("applied rollback posture is not visible: %#v", status)
	}
	rolledBack, err := fixture.manager.Rollback(context.Background(), RevisionRequest{
		OperationID: applied.OperationID, ExpectedRevision: applied.Revision})
	if err != nil || State(rolledBack.State) != StateRolledBack || rolledBack.RollbackAvailable {
		t.Fatalf("rollback=%#v err=%v", rolledBack, err)
	}
	if _, err := fixture.manager.Rollback(context.Background(), RevisionRequest{
		OperationID: rolledBack.OperationID, ExpectedRevision: rolledBack.Revision}); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("a second rollback was accepted: %v", err)
	}
}

func TestAppliedRollbackRejectsNewerAuthorityOrAnotherActiveOperation(t *testing.T) {
	t.Run("newer applied sequence", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("stale-applied-rollback"))
		if err != nil {
			t.Fatal(err)
		}
		applied, err := fixture.manager.Activate(context.Background(), RevisionRequest{
			OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err != nil {
			t.Fatal(err)
		}
		next := applied.Sequence + 1
		if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", applied.Channel).Updates(map[string]any{
			"last_observed_sequence": next,
			"last_verified_sequence": next,
			"last_applied_sequence":  next,
			"release_id":             "solovey-ui-main-newer",
			"manifest_digest":        lifecycleDigest("newer-applied-manifest"),
			"version":                "2026.5.0",
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Rollback(context.Background(), RevisionRequest{
			OperationID: applied.OperationID, ExpectedRevision: applied.Revision}); !errors.Is(err, ErrRevisionMismatch) {
			t.Fatalf("stale applied rollback error = %v", err)
		}
	})

	t.Run("another update is active", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("blocked-applied-rollback"))
		if err != nil {
			t.Fatal(err)
		}
		applied, err := fixture.manager.Activate(context.Background(), RevisionRequest{
			OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err != nil {
			t.Fatal(err)
		}
		blocker := applied
		blocker.OperationID = "update-operation:" + lifecycleDigest("new-active")[:48]
		blocker.IdempotencyKey = "new-active-update"
		blocker.State = string(StatePrepared)
		blocker.Revision = 1
		blocker.CreatedAt++
		blocker.UpdatedAt++
		if err := fixture.db.Create(&blocker).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Rollback(context.Background(), RevisionRequest{
			OperationID: applied.OperationID, ExpectedRevision: applied.Revision}); !errors.Is(err, ErrOperationConflict) {
			t.Fatalf("concurrent applied rollback error = %v", err)
		}
	})

	t.Run("another mutation domain is active", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		if err := fixture.db.AutoMigrate(&model.DataLifecycleOperation{}); err != nil {
			t.Fatal(err)
		}
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("cross-domain-applied-rollback"))
		if err != nil {
			t.Fatal(err)
		}
		applied, err := fixture.manager.Activate(context.Background(), RevisionRequest{
			OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err != nil {
			t.Fatal(err)
		}
		blocker := model.DataLifecycleOperation{OperationID: "data-operation:active", IdempotencyKey: "data-active",
			Kind: "DROP_DATA", State: "DROPPING", OwnerID: "fixture-owner", Revision: 1, CreatedAt: 1, UpdatedAt: 1}
		if err := fixture.db.Create(&blocker).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Rollback(context.Background(), RevisionRequest{
			OperationID: applied.OperationID, ExpectedRevision: applied.Revision}); !errors.Is(err, ErrOperationConflict) {
			t.Fatalf("cross-domain applied rollback error = %v", err)
		}
	})
}

func TestManifestCompatibilityRejectsPanelSchemaBrokerAndComponentDrift(t *testing.T) {
	fixture := newLifecycleFixture(t)
	base := fixture.verified.Manifest
	cases := map[string]func(*release.Manifest){
		"panel below minimum": func(manifest *release.Manifest) {
			manifest.MinimumPanelVersion = "2026.3.1"
		},
		"panel above maximum": func(manifest *release.Manifest) {
			manifest.MaximumPanelVersion = "2026.2.2"
		},
		"core outside maximum": func(manifest *release.Manifest) {
			manifest.MaximumCoreSchema = "1.10"
		},
		"target schema drift": func(manifest *release.Manifest) {
			manifest.TargetCoreSchema = "1.10"
		},
		"broker drift": func(manifest *release.Manifest) {
			manifest.BrokerCapability = "broker-capabilities-9.9"
		},
		"component range drift": func(manifest *release.Manifest) {
			manifest.Components = []release.Component{{ID: "fixture-component", Version: "1.0.0",
				ArtifactSHA256: manifest.Artifacts[0].SHA256, MinimumCoreSchema: "1.12", MaximumCoreSchema: "1.13"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := base
			mutate(&manifest)
			if err := validateManifestCompatibility(manifest); err == nil {
				t.Fatalf("incompatible manifest was accepted: %#v", manifest)
			}
		})
	}
}

func TestCheckRejectsArtifactOutsidePinnedProvenancePolicy(t *testing.T) {
	fixture := newLifecycleFixture(t)
	source := fixture.manager.sources[release.ChannelMain]
	source.ExpectedProvenance = "github-actions"
	fixture.manager.sources[release.ChannelMain] = source
	if _, err := fixture.manager.Check(context.Background(), release.ChannelMain); err == nil {
		t.Fatal("signed release with provenance outside the pinned source policy was accepted")
	}
	var states int64
	if err := fixture.db.Model(&model.UpdateReleaseState{}).Count(&states).Error; err != nil || states != 0 {
		t.Fatalf("provenance failure persisted release authority rows=%d err=%v", states, err)
	}
}

func TestStatusDoesNotPromoteStaleCurrentOrRestoredReleaseState(t *testing.T) {
	t.Run("current version is not update available", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		if err := fixture.manager.repo.saveVerified(context.Background(), fixture.verified, release.ChannelMain, false, fixture.manager.now().Unix()); err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", string(release.ChannelMain)).
			Update("version", "2026.2.3").Error; err != nil {
			t.Fatal(err)
		}
		status := fixture.manager.Status(context.Background(), release.ChannelMain)
		if status.State == StateUpdateAvailable || !containsReason(status.ReasonCodes, "installed_version_not_older") {
			t.Fatalf("current release was promoted as an update: %#v", status)
		}
	})

	t.Run("expired selection is stale", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		if err := fixture.manager.repo.saveVerified(context.Background(), fixture.verified, release.ChannelMain, true, fixture.manager.now().Unix()); err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", string(release.ChannelMain)).
			Update("expires_at", fixture.manager.now().Add(-time.Second).Unix()).Error; err != nil {
			t.Fatal(err)
		}
		status := fixture.manager.Status(context.Background(), release.ChannelMain)
		if status.State == StateUpdateAvailable || status.SigningStatus != "VERIFIED_STALE" ||
			!containsReason(status.ReasonCodes, "verified_release_metadata_stale") {
			t.Fatalf("stale release posture was promoted: %#v", status)
		}
	})

	t.Run("restored state remains untrusted", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		if err := fixture.manager.repo.saveVerified(context.Background(), fixture.verified, release.ChannelMain, true, fixture.manager.now().Unix()); err != nil {
			t.Fatal(err)
		}
		if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", string(release.ChannelMain)).
			Updates(map[string]any{"last_verified_sequence": 0, "manifest_digest": "", "signing_key_id": ""}).Error; err != nil {
			t.Fatal(err)
		}
		status := fixture.manager.Status(context.Background(), release.ChannelMain)
		if status.Selected != nil || status.State == StateUpdateAvailable ||
			!containsReason(status.ReasonCodes, "restored_release_state_untrusted") {
			t.Fatalf("restored release authority was trusted: %#v", status)
		}
	})
}

func TestMalformedPersistedUpdateAuthorityFailsClosed(t *testing.T) {
	fixture := newLifecycleFixture(t)
	operation := newUpdateOperation(fixture.prepareRequest("malformed-operation"), fixture.verified,
		fixture.verified.Manifest.Artifacts[:1], "linux", "amd64", "full", fixture.manager.now())
	operation.State = "FOREIGN_STATE"
	if err := fixture.db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	projected, err := fixture.manager.Operation(context.Background(), operation.OperationID)
	if err != nil || State(projected.State) != StateRecoveryRequired || !projected.RestoredUntrusted ||
		projected.RollbackAvailable || projected.ReasonCode != "update_operation_state_invalid" {
		t.Fatalf("malformed operation projection=%#v err=%v", projected, err)
	}
	if _, err := fixture.manager.Activate(context.Background(), RevisionRequest{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision}); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("malformed operation was actionable: %v", err)
	}
	status := fixture.manager.Status(context.Background(), release.ChannelMain)
	if status.State != StateRecoveryRequired || status.Operation == nil || !status.Operation.RestoredUntrusted {
		t.Fatalf("malformed operation status did not fail closed: %#v", status)
	}
}

func TestUpdateTimelineIsStableBoundedAndPaginated(t *testing.T) {
	fixture := newLifecycleFixture(t)
	prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("timeline"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision}); err != nil {
		t.Fatal(err)
	}
	first, truncated, err := fixture.manager.Timeline(context.Background(), prepared.OperationID, 0, 2)
	if err != nil || !truncated || len(first) != 2 || first[0].Sequence >= first[1].Sequence {
		t.Fatalf("first timeline page=%#v truncated=%v err=%v", first, truncated, err)
	}
	second, _, err := fixture.manager.Timeline(context.Background(), prepared.OperationID, first[1].Sequence, 200)
	if err != nil || len(second) == 0 || second[0].Sequence <= first[1].Sequence {
		t.Fatalf("second timeline page=%#v err=%v", second, err)
	}
	if _, _, err := fixture.manager.Timeline(context.Background(), prepared.OperationID, 0, 201); err == nil {
		t.Fatal("unbounded update timeline limit was accepted")
	}
}

func TestAppliedTransitionRollsBackWhenVerifiedIdentityDrifts(t *testing.T) {
	fixture := newLifecycleFixture(t)
	prepared, err := fixture.manager.Prepare(context.Background(), PrepareRequest{Channel: release.ChannelMain,
		ExpectedSequence: fixture.verified.Manifest.Sequence, ExpectedManifestDigest: fixture.verified.Digest,
		IdempotencyKey: "identity-drift", Acknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.UpdateReleaseState{}).Where("channel = ?", string(release.ChannelMain)).
		Update("manifest_digest", strings.Repeat("f", sha256.Size*2)).Error; err != nil {
		t.Fatal(err)
	}
	result, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
	if !errors.Is(err, ErrReleaseChanged) || State(result.State) != StateVerifyingActive {
		t.Fatalf("identity drift result=%#v err=%v", result, err)
	}
	var stored model.UpdateOperation
	if err := fixture.db.First(&stored, "operation_id = ?", prepared.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if State(stored.State) != StateVerifyingActive || stored.Revision != result.Revision {
		t.Fatalf("failed transaction changed operation: %#v", stored)
	}
	var state model.UpdateReleaseState
	if err := fixture.db.First(&state, "channel = ?", string(release.ChannelMain)).Error; err != nil {
		t.Fatal(err)
	}
	if state.LastAppliedSequence != 0 {
		t.Fatalf("failed transaction advanced applied sequence: %#v", state)
	}
	var appliedEvents int64
	if err := fixture.db.Model(&model.UpdateJournal{}).Where("operation_id = ? AND state = ?", prepared.OperationID, string(StateApplied)).Count(&appliedEvents).Error; err != nil || appliedEvents != 0 {
		t.Fatalf("failed transaction left applied journal count=%d err=%v", appliedEvents, err)
	}
}

func TestStartupAppliedReconciliationRequiresPersistedVerifiedRelease(t *testing.T) {
	fixture := newLifecycleFixture(t)
	operation := newUpdateOperation(PrepareRequest{Channel: release.ChannelMain, IdempotencyKey: "startup", Acknowledged: true},
		fixture.verified, fixture.verified.Manifest.Artifacts[:1], "linux", "amd64", "full", fixture.manager.now())
	operation.State = string(StateVerifyingActive)
	if err := fixture.manager.repo.create(context.Background(), operation, "fixture_created", ""); err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.ReconcileStartup(context.Background()); !errors.Is(err, ErrReleaseChanged) {
		t.Fatalf("startup reconciliation without release identity err=%v", err)
	}
	var stored model.UpdateOperation
	if err := fixture.db.First(&stored, "operation_id = ?", operation.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.State != string(StateVerifyingActive) || stored.Revision != operation.Revision {
		t.Fatalf("startup reconciliation falsely became terminal: %#v", stored)
	}
}

func TestLifecycleFailuresRollbackAndRecoveryAreDurable(t *testing.T) {
	t.Run("download failure", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.provider.downloadErr = errors.New("download unavailable")
		operation, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("download-failure"))
		if err == nil || State(operation.State) != StateFailed || operation.ReasonCode != "update_download_failed" {
			t.Fatalf("download failure=%#v err=%v", operation, err)
		}
		var state model.UpdateReleaseState
		if dbErr := fixture.db.First(&state, "channel = ?", string(release.ChannelMain)).Error; dbErr != nil || state.LastVerifiedSequence != fixture.verified.Manifest.Sequence {
			t.Fatalf("verified identity was not durable before download: %#v err=%v", state, dbErr)
		}
	})

	t.Run("health failure rolls back", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.provider.verifyActive = false
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("health-failure"))
		if err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err == nil || State(operation.State) != StateRolledBack {
			t.Fatalf("health failure=%#v err=%v", operation, err)
		}
		var state model.UpdateReleaseState
		if dbErr := fixture.db.First(&state, "channel = ?", string(release.ChannelMain)).Error; dbErr != nil || state.LastAppliedSequence != 0 {
			t.Fatalf("rolled-back update advanced applied sequence: %#v err=%v", state, dbErr)
		}
	})

	t.Run("rollback ambiguity requires recovery", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.provider.verifyActive = false
		fixture.provider.rollbackVerified = false
		fixture.provider.rollbackErr = errors.New("ambiguous rollback")
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("ambiguous-rollback"))
		if err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if !errors.Is(err, ErrRecoveryRequired) || State(operation.State) != StateRecoveryRequired {
			t.Fatalf("ambiguous rollback=%#v err=%v", operation, err)
		}
	})
}

func TestRestartPendingReconcilesOrTimesOutToRollback(t *testing.T) {
	t.Run("successful startup reconciliation", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.provider.verifyErr = ErrRestartPending
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("restart-reconcile"))
		if err != nil {
			t.Fatal(err)
		}
		pending, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err != nil || State(pending.State) != StateVerifyingActive {
			t.Fatalf("restart pending=%#v err=%v", pending, err)
		}
		fixture.provider.reconcileState = StateApplied
		if err := fixture.manager.ReconcileStartup(context.Background()); err != nil {
			t.Fatal(err)
		}
		stored, err := fixture.manager.Operation(context.Background(), pending.OperationID)
		if err != nil || State(stored.State) != StateApplied {
			t.Fatalf("reconciled operation=%#v err=%v", stored, err)
		}
	})

	t.Run("bounded health timeout", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.provider.verifyErr = ErrRestartPending
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("restart-timeout"))
		if err != nil {
			t.Fatal(err)
		}
		pending, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err != nil {
			t.Fatal(err)
		}
		fixture.provider.reconcileState = StateVerifyingActive
		initialNow := fixture.manager.now()
		fixture.manager.now = func() time.Time { return initialNow.Add(activationHealthDeadline + time.Second) }
		if err := fixture.manager.ReconcileStartup(context.Background()); err == nil {
			t.Fatal("health timeout was silently accepted")
		}
		stored, err := fixture.manager.Operation(context.Background(), pending.OperationID)
		if err != nil || State(stored.State) != StateRolledBack {
			t.Fatalf("timed-out operation=%#v err=%v", stored, err)
		}
	})
}

func TestRestoredUntrustedOperationAndDockerAuthorityFailClosed(t *testing.T) {
	fixture := newLifecycleFixture(t)
	prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("restored-untrusted"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.UpdateOperation{}).Where("operation_id = ?", prepared.OperationID).Update("restored_untrusted", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision}); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("restored update authority was accepted: %v", err)
	}
	caps := DockerCapabilities()
	if caps.Mode != "docker-operator-managed" || caps.Download != "UNAVAILABLE" || caps.Prepare != "UNAVAILABLE" || caps.Activate != "OPERATOR_MANAGED" || caps.OSUpdates != "EXTERNAL_MANAGED" {
		t.Fatalf("docker authority contract drifted: %#v", caps)
	}
}

func TestPrepareAdmissionAndPostRestartHealthFailClosed(t *testing.T) {
	t.Run("pressure admission", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.manager.admit = func(class string) bool { return class != "heavy_mutation" }
		if _, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("pressure-denied")); !errors.Is(err, ErrResourcePressure) {
			t.Fatalf("pressure-denied prepare err=%v", err)
		}
		var operations int64
		if err := fixture.db.Model(&model.UpdateOperation{}).Count(&operations).Error; err != nil || operations != 0 {
			t.Fatalf("pressure denial created operations=%d err=%v", operations, err)
		}
	})

	t.Run("health fence", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.manager.health = func(context.Context, model.UpdateOperation) HealthResult {
			return HealthResult{Ready: false, ReasonCodes: []string{"release_schema_identity_mismatch"}, Revision: lifecycleDigest("failed-health")}
		}
		prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("health-fence"))
		if err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.manager.Activate(context.Background(), RevisionRequest{OperationID: prepared.OperationID, ExpectedRevision: prepared.Revision})
		if err == nil || State(operation.State) != StateRolledBack || operation.ReasonCode != "active_release_health_failed" {
			t.Fatalf("health-fenced activation=%#v err=%v", operation, err)
		}
	})
}

func (fixture lifecycleFixture) prepareRequest(idempotency string) PrepareRequest {
	return PrepareRequest{Channel: release.ChannelMain, ExpectedSequence: fixture.verified.Manifest.Sequence,
		ExpectedManifestDigest: fixture.verified.Digest, IdempotencyKey: idempotency, Acknowledged: true}
}

func lifecycleDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func containsReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}
