package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestNativeFallbackLatestQueryReturnsOneIndexedRowPerRequestedResource(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	rows := []NativeFallbackOperationModel{
		nativeRetentionOperation(1, "core:inbound:17", NativeWorkflowRolledBack, 10),
		nativeRetentionOperation(2, "core:inbound:17", NativeWorkflowCancelled, 20),
		nativeRetentionOperation(3, "core:inbound:18", NativeWorkflowRolledBack, 30),
		nativeRetentionOperation(4, "core:inbound:19", NativeWorkflowRolledBack, 40),
	}
	for index := range rows {
		if err := db.Create(&rows[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := New(db)
	latest, err := repository.LatestNativeFallbackOperations(context.Background(), []string{"core:inbound:18", "core:inbound:17", "core:inbound:18"})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[0].ResourceID != "core:inbound:17" || latest[0].OperationID != rows[1].OperationID || latest[1].ResourceID != "core:inbound:18" || latest[1].OperationID != rows[2].OperationID {
		t.Fatalf("latest projection = %#v", latest)
	}
	if db.Migrator().HasIndex(&NativeFallbackOperationModel{}, "idx_sp_native_resource_latest") == false {
		t.Fatal("native latest-operation query lacks its resource/id index")
	}
	if _, err := repository.LatestNativeFallbackOperations(context.Background(), []string{"/unsafe/resource"}); err == nil {
		t.Fatal("latest query accepted an unsafe resource identity")
	}
}

func TestNativeFallbackRetentionBoundsTerminalHistoryAndPreservesFullLiveClosure(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000_000_000, 0).UTC()
	rows := make([]NativeFallbackOperationModel, 14)
	for index := range rows {
		rows[index] = nativeRetentionOperation(index, "core:inbound:17", NativeWorkflowRolledBack, now.Unix()-1000+int64(index))
		if err := db.Create(&rows[index]).Error; err != nil {
			t.Fatal(err)
		}
		mirror := nativeRetentionMirror(rows[index], "RELEASED")
		if err := db.Create(&mirror).Error; err != nil {
			t.Fatal(err)
		}
		lock := nativeRetentionLock(rows[index], "rolled_back")
		if err := db.Create(&lock).Error; err != nil {
			t.Fatal(err)
		}
	}
	state := nativeFallbackStateRow(domain.NativeActualRolledBack)
	state.ResourceID = rows[13].ResourceID
	state.OperationID = rows[13].OperationID
	state.OperationRevision = fmt.Sprint(rows[13].Revision)
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ArtifactModel{
		OperationID: rows[0].OperationID, Revision: "native-artifact-native", Scope: "native-fallback",
		RelativePath: "revisions/native-artifact-native", ManifestSHA256: strings.Repeat("d", 64), Bytes: 128,
		CreatedAt: rows[0].CreatedAt, UpdatedAt: rows[0].UpdatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&NativeFallbackOperationModel{}).Where("operation_id = ?", rows[1].OperationID).Update("core_checkpoint_released_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&FallbackTargetLeaseModel{}).Where("operation_id = ?", rows[2].OperationID).Update("state", "ACTIVE").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&OperationLockModel{}).Where("operation_id = ?", rows[3].OperationID).Update("state", "reconcile_required").Error; err != nil {
		t.Fatal(err)
	}

	result, err := New(db).PruneNativeFallbackHistory(context.Background(), 3, now.Add(-30*24*time.Hour).Unix(), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedOperations != 6 || result.DeletedLocks != 6 || result.DeletedReservations != 6 || result.Preserved != 8 || result.DeletedBytes <= 0 {
		t.Fatalf("retention result = %#v", result)
	}
	for index := 0; index < 4; index++ {
		var count int64
		if err := db.Model(&NativeFallbackOperationModel{}).Where("operation_id = ?", rows[index].OperationID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("live closure %d was pruned: count=%d err=%v", index, count, err)
		}
	}
	var currentCount int64
	if err := db.Model(&NativeFallbackOperationModel{}).Where("operation_id = ?", rows[13].OperationID).Count(&currentCount).Error; err != nil || currentCount != 1 {
		t.Fatalf("current resource operation was pruned: count=%d err=%v", currentCount, err)
	}
	var terminalCount int64
	if err := db.Model(&NativeFallbackOperationModel{}).Where("operation_id IN ?", []string{rows[4].OperationID, rows[5].OperationID, rows[6].OperationID, rows[7].OperationID, rows[8].OperationID, rows[9].OperationID, rows[10].OperationID, rows[11].OperationID, rows[12].OperationID}).Count(&terminalCount).Error; err != nil || terminalCount != 3 {
		t.Fatalf("safe terminal count = %d err=%v", terminalCount, err)
	}
	second, err := New(db).PruneNativeFallbackHistory(context.Background(), 3, now.Add(-30*24*time.Hour).Unix(), 8<<20)
	if err != nil || second.DeletedOperations != 0 || second.DeletedLocks != 0 || second.DeletedReservations != 0 {
		t.Fatalf("restart prune was not idempotent: %#v err=%v", second, err)
	}
}

func TestNativeFallbackRetentionEnforcesAgeAndLogicalBytes(t *testing.T) {
	for name, keepBytes := range map[string]int64{"age": 8 << 20, "bytes": 1} {
		t.Run(name, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(2_000_000_000, 0).UTC()
			updatedAt := now.Unix()
			if name == "age" {
				updatedAt = now.Add(-31 * 24 * time.Hour).Unix()
			}
			operation := nativeRetentionOperation(1, "core:inbound:17", NativeWorkflowCancelled, updatedAt)
			if err := db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			mirror := nativeRetentionMirror(operation, "RELEASED")
			lock := nativeRetentionLock(operation, "cancelled")
			if err := db.Create(&mirror).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&lock).Error; err != nil {
				t.Fatal(err)
			}
			result, err := New(db).PruneNativeFallbackHistory(context.Background(), 100, now.Add(-30*24*time.Hour).Unix(), keepBytes)
			if err != nil || result.DeletedOperations != 1 || result.DeletedLocks != 1 || result.DeletedReservations != 1 {
				t.Fatalf("%s retention = %#v err=%v", name, result, err)
			}
		})
	}
}

func TestNativeFallbackRetentionDeleteFaultRollsBackWholeClosure(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	operation := nativeRetentionOperation(1, "core:inbound:17", NativeWorkflowRolledBack, 1)
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	mirror := nativeRetentionMirror(operation, "RELEASED")
	lock := nativeRetentionLock(operation, "rolled_back")
	if err := db.Create(&mirror).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lock).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER native_fail_native_delete BEFORE DELETE ON server_protection_native_fallback_operations BEGIN SELECT RAISE(ABORT, 'native injected delete fault'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db).PruneNativeFallbackHistory(context.Background(), 1, 2, 8<<20); err == nil {
		t.Fatal("injected semantic delete fault was acknowledged")
	}
	for _, model := range []any{&NativeFallbackOperationModel{}, &FallbackTargetLeaseModel{}, &OperationLockModel{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("transactional rollback failed for %T: count=%d err=%v", model, count, err)
		}
	}
	if err := db.Exec(`DROP TRIGGER native_fail_native_delete`).Error; err != nil {
		t.Fatal(err)
	}
	first, err := New(db).PruneNativeFallbackHistory(context.Background(), 1, 2, 8<<20)
	if err != nil || first.DeletedOperations != 1 || first.DeletedLocks != 1 || first.DeletedReservations != 1 {
		t.Fatalf("restart prune = %#v err=%v", first, err)
	}
}

func nativeRetentionOperation(index int, resourceID, workflowState string, updatedAt int64) NativeFallbackOperationModel {
	row := nativeOperationGuardRow(workflowState)
	row.OperationID = fmt.Sprintf("native-native-%03d", index)
	row.ResourceID = resourceID
	row.ProviderReservationID = fmt.Sprintf("reservation-native-%03d", index)
	row.ProviderReservationRevision = fmt.Sprintf("reservation-revision-native-%03d", index)
	row.CoreCheckpointID = fmt.Sprintf("checkpoint-native-%03d", index)
	releasedAt := updatedAt
	row.CoreCheckpointReleasedAt = &releasedAt
	row.CreatedAt = updatedAt
	row.UpdatedAt = updatedAt
	return row
}

func nativeRetentionMirror(operation NativeFallbackOperationModel, state string) FallbackTargetLeaseModel {
	row := nativeMirrorGuardRow(state)
	row.LeaseID = operation.ProviderReservationID
	row.HolderID = operation.OperationID
	row.OperationID = operation.OperationID
	row.ResourceID = operation.ResourceID
	row.ProviderReservationID = operation.ProviderReservationID
	row.ProviderReservationRevision = operation.ProviderReservationRevision
	row.IssuedAt = operation.CreatedAt
	row.RenewedAt = operation.UpdatedAt
	row.ExpiresAt = operation.UpdatedAt + 60
	if state == "RELEASED" {
		row.ReleasedAt = operation.UpdatedAt
	}
	return row
}

func nativeRetentionLock(operation NativeFallbackOperationModel, state string) OperationLockModel {
	pid := 42
	return OperationLockModel{
		OperationID: operation.OperationID, Kind: nativeFallbackSharedOperationKind, ResourceID: operation.ResourceID,
		State: state, Revision: operation.Revision, IdempotencyKey: "idempotency-" + operation.OperationID,
		PlanRevision: operation.PlanDigest, LockedByPID: &pid, LockedByInstanceID: "native-instance", Actor: "native-test",
		HeartbeatAt: operation.UpdatedAt, ExpiresAt: operation.UpdatedAt, CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt,
	}
}
