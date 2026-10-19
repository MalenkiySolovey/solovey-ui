package update

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestRealUpdateChangeProjectionPreservesRollbackTruth(t *testing.T) {
	for _, rollbackVerified := range []bool{true, false} {
		t.Run(map[bool]string{true: "verified", false: "recovery"}[rollbackVerified], func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			prepared, err := fixture.manager.Prepare(context.Background(), fixture.prepareRequest("change-proof"))
			if err != nil {
				t.Fatal(err)
			}
			before := fixture.manager.Status(context.Background(), release.ChannelMain).Change
			if before == nil || before.OwnerPhase != string(StatePrepared) || before.Outcome != operationcoordination.OutcomePending || before.CheckpointRef != prepared.BackupRef {
				t.Fatalf("prepared change: %+v", before)
			}
			fixture.provider.activateErr = errors.New("owner activation failed")
			fixture.provider.rollbackVerified = rollbackVerified
			result, err := fixture.manager.Activate(context.Background(), RevisionRequest{prepared.OperationID, prepared.Revision})
			if err == nil {
				t.Fatal("activation failure disappeared")
			}
			change := ProjectChange(result)
			if change.ChangeID != before.ChangeID || change.RollbackVerified != rollbackVerified || change.RecoveryRequired == rollbackVerified {
				t.Fatalf("rollback projection: %+v", change)
			}
			if _, err := fixture.manager.Activate(context.Background(), RevisionRequest{prepared.OperationID, prepared.Revision}); !errors.Is(err, ErrRevisionMismatch) {
				t.Fatalf("stale activation fence: %v", err)
			}
		})
	}
}
