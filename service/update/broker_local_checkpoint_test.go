package update

import (
	"errors"
	"testing"

	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func TestLiveRollbackRetentionAcceptsOnlyBoundRootLocalInstallerCheckpoint(t *testing.T) {
	operationID, manifest := "update-operation:installer-transition", lifecycleDigest("target signed manifest")
	local := contract.LocalGenerationCheckpointV1{Kind: "installer-root-owned/v1", Digest: lifecycleDigest("local installer contents")}
	result := contract.ObservationV1{ProviderRevision: contract.ProviderRevision, ManagementReady: true, RollbackAvailable: true,
		RollbackOperationID: operationID, RollbackManifestDigest: manifest, RollbackRef: contract.SemanticReference("rollback", operationID, manifest),
		RollbackTargetLocalCheckpoint: local}
	if keep, err := liveRollbackOperation(result); err != nil || keep != operationID {
		t.Fatal("Root's local rollback target lost its retained database checkpoint", err)
	}
	for _, mutate := range []func(*contract.ObservationV1){
		func(v *contract.ObservationV1) { v.RollbackTargetLocalCheckpoint.Kind = "unknown/v1" },
		func(v *contract.ObservationV1) { v.RollbackTargetLocalCheckpoint.Digest = "" },
		func(v *contract.ObservationV1) {
			v.RollbackTargetSequence, v.RollbackTargetDigest = 7, lifecycleDigest("signed")
		},
		func(v *contract.ObservationV1) { v.RollbackRef = lifecycleDigest("unbound") },
	} {
		changed := result
		mutate(&changed)
		if keep, err := liveRollbackOperation(changed); keep != "" || !errors.Is(err, ErrRecoveryRequired) {
			t.Fatal("unbound or mixed local rollback target retained authority", err)
		}
	}
}
