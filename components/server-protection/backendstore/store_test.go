package backendstore

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func durableOwnerFixture(t *testing.T) (*gorm.DB, *Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "owner.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db, New(repository.New(db))
}

func TestTypedJournalPreservesOwnerCASAndRejectsExpiredPreparedAdmission(t *testing.T) {
	_, store := durableOwnerFixture(t)
	input := durable.AcquireOperationLockInput{
		OperationID: "operation-owned", Kind: "firewall", State: "prepared", ResourceID: "owned:surface",
		IdempotencyKey: "request-owned", LockedByPID: 111, LockedByInstanceID: "instance-owned", Actor: "operator",
		PlanRevision: "accepted-plan", HelperRevision: "accepted-helper", Now: 1000, ExpiresAt: 1100,
	}
	first, joined, err := store.AcquireOperationLock(t.Context(), input)
	if err != nil || joined || first.Revision != 1 || first.LockedByPID == nil || *first.LockedByPID != 111 {
		t.Fatalf("typed acquire lost owner fence: %#v joined=%v err=%v", first, joined, err)
	}
	replay, joined, err := store.AcquireOperationLock(t.Context(), input)
	if err != nil || !joined || replay.ID != first.ID {
		t.Fatalf("typed replay changed operation: %#v joined=%v err=%v", replay, joined, err)
	}
	update := durable.FencedOperationLockUpdate{OperationID: first.OperationID, Revision: first.Revision,
		InstanceID: "instance-foreign", PID: 111, FromStates: []string{"prepared"}, ToState: "applying", Now: 1001, ExpiresAt: 1200}
	if _, err := store.UpdateOperationLockFenced(t.Context(), update); !errors.Is(err, durable.ErrOperationFenced) {
		t.Fatalf("foreign instance obtained the operation: %v", err)
	}
	update.InstanceID, update.Now = input.LockedByInstanceID, input.ExpiresAt
	if _, err := store.UpdateOperationLockFenced(t.Context(), update); !errors.Is(err, durable.ErrOperationFenced) {
		t.Fatalf("expired prepared admission revived: %v", err)
	}
	update.Now = 1001
	applied, err := store.UpdateOperationLockFenced(t.Context(), update)
	if err != nil || applied.Revision != first.Revision+1 || applied.State != "applying" || applied.PlanRevision != input.PlanRevision {
		t.Fatalf("valid owner transition lost its identity: %#v err=%v", applied, err)
	}
	if _, err := store.UpdateOperationLockFenced(t.Context(), update); !errors.Is(err, durable.ErrOperationFenced) {
		t.Fatalf("old revision survived transition: %v", err)
	}
}

func TestTypedArtifactWriteRetainsBackendGeneratedIdentity(t *testing.T) {
	_, store := durableOwnerFixture(t)
	artifact := durable.ArtifactModel{OperationID: "operation-owned", Revision: "artifact-owned", Scope: "revision",
		RelativePath: "revisions/artifact-owned", ManifestSHA256: "manifest-owned", Bytes: 123, CreatedAt: 1000, UpdatedAt: 1000}
	if err := store.SaveArtifact(t.Context(), &artifact); err != nil {
		t.Fatal(err)
	}
	actual, err := store.ArtifactByOperation(t.Context(), artifact.OperationID)
	if err != nil || artifact.ID == 0 || actual != artifact {
		t.Fatalf("Backend generated identity was lost: write=%#v read=%#v err=%v", artifact, actual, err)
	}
}

func TestTypedAuthorityRetainsPersistedRuntimeEvidenceAndEmptyContributions(t *testing.T) {
	db, store := durableOwnerFixture(t)
	composition := repository.FirewallCompositionModel{ID: 1, Schema: "composition-schema", Revision: "composition-owned",
		ManagedPlanRevision: "plan-owned", CandidateSHA256: "candidate-owned", CandidateSemanticSHA256: "semantic-owned",
		CandidateTimedMembershipSHA256: "membership-owned", BindingsJSON: []byte("[]"), State: "ACTIVE", AppliedOperationID: "operation-owned", UpdatedAt: 1000,
		Runtime: repository.FirewallRuntimeBinding{Schema: repository.FirewallRuntimeSchema, VerifiedBoot: "verified-boot", AttemptBoot: "attempt-boot",
			State: "RESTORE_REQUIRED", Reason: "boot-change", StartedAt: 10, Deadline: 20, MutationAt: 12, HealthAt: 13, HealthRevision: "health-owned",
			ArtifactRevision: "artifact-owned", ArtifactSHA256: "artifact-digest", ArtifactMembershipSHA256: "artifact-membership", CleanupAttempted: true}}
	if err := db.Create(&composition).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.FirewallAuthority(t.Context())
	if err != nil || !snapshot.HasComposition || len(snapshot.Contributions) != 0 || snapshot.HasObservation ||
		snapshot.Composition.Runtime != durable.FirewallRuntimeBinding(composition.Runtime) || snapshot.Composition.Revision != composition.Revision ||
		snapshot.Composition.CandidateTimedMembershipSHA256 != composition.CandidateTimedMembershipSHA256 {
		t.Fatalf("typed projection altered durable authority: %#v err=%v", snapshot, err)
	}
}
