package firewall

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

func TestMissingRecoveryBundleOwnerRejectsBeforeRollback(t *testing.T) {
	backend := BackendRecovery{Helper: struct{ Helper }{}, Manager: &operations.Manager{}, Storage: struct{ RecoveryArtifacts }{}, Repository: struct{ RecoveryStore }{}, Workflow: &Workflow{Codec: struct{ NFTCodec }{}}}
	if err := backend.AttemptRollback(t.Context(), durable.OperationLockModel{}); err == nil {
		t.Fatal("rollback admitted without recovery evidence owner")
	}
	if err := backend.CreateBundle(t.Context(), durable.OperationLockModel{}, "rollback_failed"); err == nil {
		t.Fatal("missing recovery bundle owner accepted")
	}
}
