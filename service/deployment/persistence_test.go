package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func TestPosturePreservesDurableIntentAndDoctor(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	ctx := context.Background()
	for _, field := range []string{"desired_profile", "generated_profile", "generated_revision"} {
		t.Run(field, func(t *testing.T) {
			if err := db.Where("scope = ?", "global").Delete(&model.DeploymentState{}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Status(ctx); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.DeploymentState{}).Where("scope = ?", "global").Updates(map[string]any{
				"desired_profile": "preserved-desired", "generated_profile": "preserved-generated", "generated_revision": "preserved-revision", "doctor_revision": "preserved-doctor", field: "",
			}).Error; err != nil {
				t.Fatal(err)
			}
			posture := provider.posture(domain.NativeHardened)
			if err := manager.Repository.SavePosture(ctx, posture, false); err != nil {
				t.Fatal(err)
			}
			state, err := manager.Repository.State(ctx)
			if err != nil {
				t.Fatal(err)
			}
			profile, _ := domain.Lookup(posture.Profile)
			want := map[string]string{"desired_profile": "preserved-desired", "generated_profile": "preserved-generated", "generated_revision": "preserved-revision"}
			want[field] = string(posture.Profile)
			if field == "generated_revision" {
				want[field] = profile.Revision
			}
			if state.DesiredProfile != want["desired_profile"] || state.GeneratedProfile != want["generated_profile"] || state.GeneratedRevision != want["generated_revision"] || state.DoctorRevision != "preserved-doctor" {
				t.Fatalf("durable authority changed: %+v", state)
			}
			if state.ProfileID != string(posture.Profile) || state.PostureRevision != posture.Revision || state.Trusted || state.ObservedAt != posture.ObservedAt || state.ActiveProfile != string(posture.ActiveProfile) || state.InstalledProfile != string(posture.InstalledProfile) || state.VerifiedProfile != string(posture.VerifiedProfile) {
				t.Fatal("observed posture was not fully updated")
			}
		})
	}
}

func TestPersistenceDiagnosticIsBoundedAndUnwraps(t *testing.T) {
	for _, tc := range []struct {
		code     sqlite3.ErrNo
		extended sqlite3.ErrNoExtended
		class    string
	}{
		{sqlite3.ErrBusy, sqlite3.ErrBusySnapshot, "busy"}, {sqlite3.ErrLocked, sqlite3.ErrLockedSharedCache, "locked"},
		{sqlite3.ErrReadonly, sqlite3.ErrReadonlyRecovery, "readonly"}, {sqlite3.ErrConstraint, sqlite3.ErrConstraintTrigger, "constraint"},
		{sqlite3.ErrSchema, sqlite3.ErrNoExtended(sqlite3.ErrSchema), "schema"}, {sqlite3.ErrIoErr, sqlite3.ErrIoErrRead, "io"},
	} {
		original := sqlite3.Error{Code: tc.code, ExtendedCode: tc.extended}
		err := persistenceFailure(doctorSave, fmt.Errorf("private SQL and path: %w", original))
		diagnostic, ok := PersistenceFailure(err)
		var retained sqlite3.Error
		if !ok || diagnostic.Stage != "doctor_save" || diagnostic.Class != tc.class || diagnostic.SQLiteExtendedCode != int(tc.extended) || !errors.Is(err, ErrStatePersistence) || !errors.As(err, &retained) || retained != original || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe or lossy diagnostic: %v", err)
		}
	}
	unknown := errors.New("private row and path")
	err := persistenceFailure(stateRead, unknown)
	diagnostic, ok := PersistenceFailure(err)
	if !ok || diagnostic.Class != "unknown" || !errors.Is(err, unknown) || strings.Contains(err.Error(), "private") {
		t.Fatal("unknown failure not safely preserved")
	}
	if persistenceFailure(stateRead, gorm.ErrRecordNotFound) != gorm.ErrRecordNotFound {
		t.Fatal("absence changed into storage failure")
	}
}

func TestDoctorPersistenceFailureStagesAndAtomicRollback(t *testing.T) {
	for _, stage := range []string{"posture_save", "state_read", "recovery_read", "doctor_save"} {
		t.Run(stage, func(t *testing.T) {
			manager, _, db := deploymentFixture(t)
			ctx := context.Background()
			if _, err := manager.Status(ctx); err != nil {
				t.Fatal(err)
			}
			fault := sqlite3.Error{Code: sqlite3.ErrIoErr, ExtendedCode: sqlite3.ErrIoErrRead}
			queryCount := 0
			if err := db.Callback().Query().Before("gorm:query").Register("failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "deployment_state_v1" {
					queryCount++
					if stage == "state_read" {
						tx.AddError(fault)
					}
				}
				if stage == "recovery_read" && tx.Statement.Table == "deployment_operations_v1" {
					tx.AddError(fault)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if stage == "posture_save" {
				if err := db.Exec(`CREATE TRIGGER persistence_failure BEFORE INSERT ON deployment_state_v1 BEGIN SELECT RAISE(ABORT, 'private SQL row path'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if stage == "doctor_save" {
				// Fail after the snapshot insert to prove both snapshot and pointer roll back.
				if err := db.Exec(`CREATE TRIGGER persistence_failure BEFORE UPDATE OF doctor_revision ON deployment_state_v1 BEGIN SELECT RAISE(ABORT, 'private SQL row path'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			_, err := manager.Doctor(ctx)
			diagnostic, ok := PersistenceFailure(err)
			if !ok || diagnostic.Stage != stage || !errors.Is(err, ErrStatePersistence) || strings.Contains(err.Error(), "private") {
				t.Fatalf("stage=%s diagnostic=%+v err=%v", stage, diagnostic, err)
			}
			var count int64
			if err := db.Model(&model.DeploymentDoctorSnapshot{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("partial snapshot persisted count=%d err=%v", count, err)
			}
			if stage == "doctor_save" {
				var state model.DeploymentState
				if err := db.Take(&state).Error; err != nil || state.DoctorRevision != "" {
					t.Fatal("partial doctor pointer persisted")
				}
			}
			_ = queryCount
		})
	}
}

func TestDoctorRetentionKeepsCurrentAuthorityOnOlderReplay(t *testing.T) {
	manager, _, db := deploymentFixture(t)
	ctx := context.Background()
	if _, err := manager.Status(ctx); err != nil {
		t.Fatal(err)
	}
	old := domain.FinalizeDoctor(domain.DoctorReport{GeneratedAt: 1})
	for index := 1; index <= maxDoctorSnapshots+2; index++ {
		report := domain.FinalizeDoctor(domain.DoctorReport{GeneratedAt: int64(index)})
		if err := manager.Repository.SaveDoctor(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := manager.Repository.SaveDoctor(ctx, old); err != nil {
			t.Fatal(err)
		}
		state, err := manager.Repository.State(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot model.DeploymentDoctorSnapshot
		if err := db.Where("revision = ?", state.DoctorRevision).Take(&snapshot).Error; err != nil {
			t.Fatalf("current doctor points at pruned snapshot: %v", err)
		}
		var count int64
		if err := db.Model(&model.DeploymentDoctorSnapshot{}).Count(&count).Error; err != nil || count != maxDoctorSnapshots {
			t.Fatalf("retention count=%d err=%v", count, err)
		}
	}
}
