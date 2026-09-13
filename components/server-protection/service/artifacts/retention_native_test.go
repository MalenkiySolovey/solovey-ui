package artifacts

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNativeHistoryRunsThroughRestartRetentionOwner(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "native-retention.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		operationID := fmt.Sprintf("native-native-integrated-%d", index)
		digest := strings.Repeat(fmt.Sprint(index+1), 64)
		operation := protectionrepository.NativeFallbackOperationModel{
			Schema: protectionrepository.NativeFallbackOperationSchemaV1, OperationID: operationID, Revision: 1,
			ResourceID: "core:inbound:28", InboundDatabaseID: 28, PlanID: digest, PlanDigest: digest, PlanJSON: []byte(`{}`),
			RuntimeIdentityRevision: digest, CapabilityResolverRevision: digest, BeforeConfigurationRevision: digest,
			ExpectedAfterRevision: digest, BeforeEffectiveRevision: digest, TargetReferenceJSON: []byte(`{}`), TargetRevision: digest,
			ProviderRevision: "provider", EndpointRevision: digest, PublishRevision: "publish", HealthRevision: digest,
			CapacityRevision: digest, WorkflowState: protectionrepository.NativeWorkflowCancelled, HealthFactsJSON: []byte(`{}`),
			ReasonCodesJSON: []byte(`[]`), RecoveryBundleJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1,
		}
		if err := db.Create(&operation).Error; err != nil {
			t.Fatal(err)
		}
		pid := 42
		lock := protectionrepository.OperationLockModel{
			OperationID: operationID, Kind: "native_fallback", ResourceID: operation.ResourceID, State: "cancelled", Revision: 1,
			IdempotencyKey: "idempotency-" + operationID, LockedByPID: &pid, LockedByInstanceID: "native", Actor: "native",
			HeartbeatAt: 1, ExpiresAt: 1, CreatedAt: 1, UpdatedAt: 1,
		}
		if err := db.Create(&lock).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := protectionrepository.New(db)
	storage := artifactTestStorage(t, now)
	result, err := NewPruner(storage, repository, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedNativeOperations != 2 || result.DeletedNativeLocks != 2 || result.DeletedNativeReservations != 0 {
		t.Fatalf("integrated native prune = %#v", result)
	}
	second, err := NewPruner(storage, repository, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil || second.DeletedNativeOperations != 0 || second.DeletedNativeLocks != 0 {
		t.Fatalf("integrated native restart prune = %#v err=%v", second, err)
	}
}
