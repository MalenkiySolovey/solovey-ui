package nativefallback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type nativeCoreCoordinator struct{}

func (nativeCoreCoordinator) RunBlockingContext(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return operation()
}

type nativeCoreRuntime struct{}

func (nativeCoreRuntime) ApplyInbound(context.Context, uint) (coreinboundcontrol.RuntimeInboundObservationV1, error) {
	return coreinboundcontrol.RuntimeInboundObservationV1{}, nil
}

func (nativeCoreRuntime) ObserveInbound(context.Context, string) (coreinboundcontrol.RuntimeInboundObservationV1, error) {
	return coreinboundcontrol.RuntimeInboundObservationV1{}, nil
}

func TestNativeRetentionConsumesProductionCheckpointRelease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "native-core-checkpoint.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Client{}, &model.InboundFallbackCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Id: 28, Type: "vless", Tag: "native-core-inbound", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":24443}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	core := coreinboundcontrol.NewWithMutations(db, nil, coreinboundcontrol.MutationDependencies{
		Coordinator: nativeCoreCoordinator{}, Runtime: nativeCoreRuntime{},
	})
	snapshot, err := core.Snapshot(t.Context(), inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := "native-core-checkpoint"
	checkpointPayload := struct {
		Schema                      string                                    `json:"schema"`
		CheckpointID                string                                    `json:"checkpointId"`
		PreviewDigest               string                                    `json:"previewDigest"`
		InboundDatabaseID           uint                                      `json:"inboundDatabaseId"`
		Variant                     coreinboundcontrol.FallbackPatchVariantV1 `json:"variant"`
		BeforeConfigurationRevision string                                    `json:"beforeConfigurationRevision"`
		ExpectedAfterRevision       string                                    `json:"expectedAfterRevision"`
		RuntimeIdentityRevision     string                                    `json:"runtimeIdentityRevision"`
		EndpointProviderID          string                                    `json:"endpointProviderId"`
		EndpointID                  string                                    `json:"endpointId"`
		EndpointRevision            string                                    `json:"endpointRevision"`
		EndpointBindingDigest       string                                    `json:"endpointBindingDigest"`
		CreatedAt                   time.Time                                 `json:"createdAt"`
		ExpiresAt                   time.Time                                 `json:"expiresAt"`
	}{
		Schema: coreinboundcontrol.FallbackCheckpointSchemaV1, CheckpointID: checkpointID,
		PreviewDigest: strings.Repeat("a", 64), InboundDatabaseID: inbound.Id,
		Variant:                     coreinboundcontrol.FallbackPatchVLESSRealityHandshakeTCP,
		BeforeConfigurationRevision: snapshot.ConfigurationRevision, ExpectedAfterRevision: strings.Repeat("b", 64),
		RuntimeIdentityRevision: snapshot.RuntimeIdentityRevision, EndpointProviderID: "native-provider", EndpointID: "native-endpoint",
		EndpointRevision: strings.Repeat("c", 64), EndpointBindingDigest: strings.Repeat("d", 64),
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	payload, err := json.Marshal(checkpointPayload)
	if err != nil {
		t.Fatal(err)
	}
	checkpointDigest := nativeSHA256(payload)
	releaseProof := nativeTerminalProof(checkpointID, "prepared", snapshot.ConfigurationRevision)
	if err := db.Create(&model.InboundFallbackCheckpoint{
		ID: checkpointID, Schema: coreinboundcontrol.FallbackCheckpointSchemaV1, InboundID: inbound.Id,
		Payload: payload, IntegrityDigest: checkpointDigest, State: "prepared", CreatedAtUnix: time.Now().Unix(), ExpiresAtUnix: time.Now().Add(5 * time.Minute).Unix(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("e", 64)
	operation := protectionrepository.NativeFallbackOperationModel{
		Schema: protectionrepository.NativeFallbackOperationSchemaV1, OperationID: "native-native-core-checkpoint", Revision: 1,
		ResourceID: "core:inbound:28", InboundDatabaseID: inbound.Id, PlanID: digest, PlanDigest: digest, PlanJSON: []byte(`{}`),
		RuntimeIdentityRevision: snapshot.RuntimeIdentityRevision, CapabilityResolverRevision: snapshot.CapabilityResolverRevision,
		BeforeConfigurationRevision: snapshot.ConfigurationRevision, ExpectedAfterRevision: digest, BeforeEffectiveRevision: digest,
		TargetReferenceJSON: []byte(`{}`), TargetRevision: digest, ProviderRevision: "provider", EndpointRevision: digest,
		PublishRevision: "publish", HealthRevision: digest, CapacityRevision: digest,
		CoreCheckpointID: checkpointID, CoreCheckpointDigest: checkpointDigest, CheckpointReleaseProof: releaseProof,
		WorkflowState: protectionrepository.NativeWorkflowCancelled, HealthFactsJSON: []byte(`{}`), ReasonCodesJSON: []byte(`[]`),
		RecoveryBundleJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1,
	}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	pid := 42
	if err := db.Create(&protectionrepository.OperationLockModel{
		OperationID: operation.OperationID, Kind: "native_fallback", ResourceID: operation.ResourceID, State: "cancelled", Revision: 1,
		IdempotencyKey: "native-production-checkpoint", LockedByPID: &pid, LockedByInstanceID: "native", Actor: "native",
		HeartbeatAt: 1, ExpiresAt: 1, CreatedAt: 1, UpdatedAt: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	const expiredCutoff = int64(2_000_000_000)
	before, err := repository.PruneNativeFallbackHistory(context.Background(), 1, expiredCutoff, 8<<20)
	if err != nil || before.DeletedOperations != 0 || before.Preserved != 1 {
		t.Fatalf("unreleased production checkpoint was not protected: %#v err=%v", before, err)
	}
	released, err := core.ReleaseCheckpoint(t.Context(), coreinboundcontrol.ReleaseCheckpointRequestV1{
		CheckpointID: checkpointID, Kind: coreinboundcontrol.CheckpointProofApplyNeverCommitted, ProofDigest: releaseProof,
	})
	if err != nil {
		t.Fatal(err)
	}
	releasedAt := released.ReleasedAt.Unix()
	if err := db.Model(&protectionrepository.NativeFallbackOperationModel{}).Where("operation_id = ?", operation.OperationID).Update("core_checkpoint_released_at", releasedAt).Error; err != nil {
		t.Fatal(err)
	}
	after, err := repository.PruneNativeFallbackHistory(context.Background(), 1, expiredCutoff, 8<<20)
	if err != nil || after.DeletedOperations != 1 || after.DeletedLocks != 1 {
		t.Fatalf("released production checkpoint did not close retention: %#v err=%v", after, err)
	}
}

func nativeSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func nativeTerminalProof(checkpointID, state, revision string) string {
	content, _ := json.Marshal(struct{ Schema, CheckpointID, State, Revision string }{
		"solovey-ui/inbound-fallback-terminal-proof/v1", checkpointID, state, revision,
	})
	return nativeSHA256(content)
}
