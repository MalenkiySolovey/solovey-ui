package update

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func (p *BrokerProvider) AdmitOrdinaryActivation(ctx context.Context, operation model.UpdateOperation, current release.CoreProviderSource) error {
	observed, err := p.observeLiveRollback(ctx, operation)
	if err != nil || observed.ProviderRevision != contract.ProviderRevision || !observed.ManagementReady {
		return ErrProviderUnavailable
	}
	if observed.ProviderMaintenance != nil && observed.ProviderMaintenance.OperationID == operation.OperationID {
		return ErrCoreProviderMaintenance
	}
	if observed.OrdinaryUpdate == nil {
		// An existing v1 checkpoint has no provider declaration. Its original
		// Official-only activation contract remains closed to Extended.
		if current.ID != "sing-box" {
			return ErrCoreProviderMaintenance
		}
		return nil
	}
	identity := releaseIdentityFromOperation(operation)
	identity.CoreProvider = observed.OrdinaryUpdate.CoreProvider
	if observed.OrdinaryUpdate.InstalledProvider != current || !observed.OrdinaryUpdate.Matches(operation.OperationID, identity) ||
		!observed.PreparedRollbackAvailable || observed.PreparedRollbackOperationID != operation.OperationID ||
		observed.PreparedRollbackManifestDigest != operation.ManifestDigest ||
		observed.PreparedRollbackRef != updateRef(operation, "rollback") {
		return ErrCoreProviderMaintenance
	}
	return nil
}
