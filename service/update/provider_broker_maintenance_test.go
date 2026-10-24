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
