package update

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func TestProviderMaintenanceModeBelongsToOneUpdateOperation(t *testing.T) {
	previous := model.UpdateOperation{OperationID: "update-operation:previous"}
	next := model.UpdateOperation{OperationID: "update-operation:next"}
	provider := &BrokerProvider{observeRollbackAuthority: func(context.Context, model.UpdateOperation) (contract.ObservationV1, error) {
		return contract.ObservationV1{ProviderMaintenance: &contract.ProviderMaintenanceObservationV1{OperationID: previous.OperationID}}, nil
	}}
	provider.requireProviderMaintenance(previous)
	if !provider.providerMaintenanceFor(previous) || provider.providerMaintenanceFor(next) {
		t.Fatal("a later operation inherited provider-only rollback mode")
	}
	if proof, err := provider.providerProof(t.Context(), next); err != nil || proof != nil {
		t.Fatal("completed foreign checkpoint affected an ordinary operation", err)
	}
	provider.requireProviderMaintenance(next)
	if proof, err := provider.providerProof(t.Context(), next); !errors.Is(err, ErrRecoveryRequired) || proof != nil {
		t.Fatal("known provider maintenance fell back when its own checkpoint disappeared", err)
	}
}

func TestProviderPreparedResultAcceptsOnlyRootCheckpointReferences(t *testing.T) {
	operation := model.UpdateOperation{OperationID: "update-operation:root-reference-binding", ManifestDigest: lifecycleDigest("root-manifest")}
	for _, validated := range []bool{false, true} {
		result := contract.PrepareResultV1{ProviderRevision: contract.ProviderRevision, ManagementReady: true, RollbackAvailable: true,
			PreparedRef: contract.SemanticReference("prepared", operation.OperationID, operation.ManifestDigest),
			RollbackRef: contract.SemanticReference("rollback", operation.OperationID, operation.ManifestDigest), ConfigurationVerified: validated}
		if !providerPreparedResult(operation, result, validated) {
			t.Fatal("the actual Root checkpoint encoding was rejected by the Panel owner")
		}
		if providerPreparedResult(operation, result, !validated) {
			t.Fatal("configuration validation state was not bound")
		}
		result.RollbackRef = contract.SemanticReference("rollback", operation.OperationID, lifecycleDigest("other-manifest"))
		if providerPreparedResult(operation, result, validated) {
			t.Fatal("another Root checkpoint was accepted")
		}
	}
}
