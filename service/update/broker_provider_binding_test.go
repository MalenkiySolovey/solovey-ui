package update

import (
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func TestRollbackReconciliationConsumesOnlyExactPreparedRollbackBinding(t *testing.T) {
	operation := model.UpdateOperation{OperationID: "update-operation:rollback-exact-binding", Sequence: 12, ManifestDigest: lifecycleDigest("rollback-manifest")}
	rollbackRef := updateRef(operation, "rollback")

	for name, observation := range map[string]contract.ObservationV1{
		"unrelated-live-authority": {
			RollbackAvailable: true, RollbackOperationID: "update-operation:older", RollbackManifestDigest: lifecycleDigest("older"),
			RollbackRef: lifecycleDigest("older-ref"),
		},
		"wrong-prepared-operation": {
			PreparedRollbackAvailable: true, PreparedRollbackOperationID: "update-operation:other",
			PreparedRollbackManifestDigest: operation.ManifestDigest, PreparedRollbackRef: rollbackRef,
		},
		"wrong-prepared-manifest": {
			PreparedRollbackAvailable: true, PreparedRollbackOperationID: operation.OperationID,
			PreparedRollbackManifestDigest: lifecycleDigest("other-manifest"), PreparedRollbackRef: rollbackRef,
		},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := reconcileBrokerObservation(operation, observation)
			if state != StateRecoveryRequired || !errors.Is(err, ErrRecoveryRequired) {
				t.Fatalf("state=%s err=%v observation=%#v", state, err, observation)
			}
		})
	}

	exact := contract.ObservationV1{PreparedRollbackAvailable: true, PreparedRollbackOperationID: operation.OperationID,
		PreparedRollbackManifestDigest: operation.ManifestDigest, PreparedRollbackRef: rollbackRef}
	if state, err := reconcileBrokerObservation(operation, exact); state != StateRollbackPending || err != nil {
		t.Fatalf("exact prepared checkpoint state=%s err=%v", state, err)
	}
	exact.ActiveSequence, exact.ActiveDigest = operation.Sequence, operation.ManifestDigest
	if state, err := reconcileBrokerObservation(operation, exact); state != StateVerifyingActive || err != nil {
		t.Fatalf("exact active release state=%s err=%v", state, err)
	}
	exact.VerifiedSequence, exact.VerifiedDigest, exact.ManagementReady = operation.Sequence, operation.ManifestDigest, true
	if state, err := reconcileBrokerObservation(operation, exact); state != StateApplied || err != nil {
		t.Fatalf("exact verified release state=%s err=%v", state, err)
	}
}
