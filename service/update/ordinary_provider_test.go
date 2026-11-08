package update

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func TestOrdinaryActivationUsesTheExistingRootProviderAndRollbackBinding(t *testing.T) {
	for _, id := range []string{"sing-box", "shtorm-extended"} {
		t.Run(id, func(t *testing.T) {
			fixture, _ := providerTransitionFixture(t)
			artifacts, err := fixture.verified.PanelArtifactsForProvider("linux", "amd64", "full", id)
			if err != nil {
				t.Fatal(err)
			}
			op := newUpdateOperation(fixture.prepareRequest("ordinary-root"), fixture.verified, artifacts, "linux", "amd64", "full", fixture.manager.now())
			source := updateTestCoreProvider(t, id)().Source
			proof := contract.OrdinaryUpdateV1{OperationID: op.OperationID, ManifestDigest: op.ManifestDigest, ArtifactSetDigest: op.ArtifactSetDigest, Sequence: op.Sequence, Version: op.Version, BinaryProfile: op.BinaryProfile, CoreProvider: source}
			proof.InstalledProvider = source
			raw, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			proof.Revision = broker.Digest(raw)
			observed := contract.ObservationV1{ProviderRevision: contract.ProviderRevision, ManagementReady: true, OrdinaryUpdate: &proof, PreparedRollbackAvailable: true, PreparedRollbackOperationID: op.OperationID, PreparedRollbackManifestDigest: op.ManifestDigest, PreparedRollbackRef: updateRef(op, "rollback")}
			provider := &BrokerProvider{observeRollbackAuthority: func(context.Context, model.UpdateOperation) (contract.ObservationV1, error) { return observed, nil }}
			if err := provider.AdmitOrdinaryActivation(t.Context(), op, source); err != nil {
				t.Fatal("current root binding rejected", err)
			}
			original := observed
			for _, change := range []func(){
				func() { observed.ProviderRevision = lifecycleDigest("older-provider-abi") },
				func() { observed.ManagementReady = false },
				func() { observed.PreparedRollbackAvailable = false },
				func() { observed.PreparedRollbackOperationID += "-other" },
				func() { observed.PreparedRollbackManifestDigest = lifecycleDigest("other-manifest") },
				func() { observed.PreparedRollbackRef = lifecycleDigest("other-ref") },
				func() {
					observed.ProviderMaintenance = &contract.ProviderMaintenanceObservationV1{OperationID: op.OperationID}
				},
			} {
				observed = original
				change()
				if err := provider.AdmitOrdinaryActivation(t.Context(), op, source); err == nil {
					t.Fatal("mixed or stale root authority was admitted")
				}
			}
			observed = original
			other := "sing-box"
			if id == other {
				other = "shtorm-extended"
			}
			if err := provider.AdmitOrdinaryActivation(t.Context(), op, updateTestCoreProvider(t, other)().Source); err == nil {
				t.Fatal("provider changed between prepare and activate")
			}
			observed.OrdinaryUpdate = nil
			if err := provider.AdmitOrdinaryActivation(t.Context(), op, updateTestCoreProvider(t, "shtorm-extended")().Source); err == nil {
				t.Fatal("missing v2 proof became legacy Extended permission")
			}
		})
	}
}
