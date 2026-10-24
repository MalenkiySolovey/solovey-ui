package update

import (
	"context"
	"errors"
	"os"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func (p *BrokerProvider) setCoreConfigurationOwner(owner CoreConfigurationOwner) {
	p.configuration = owner
}

func (p *BrokerProvider) requireProviderMaintenance(operation model.UpdateOperation) {
	p.providerMaintenanceOperation = operation.OperationID
}

func (p *BrokerProvider) providerMaintenanceFor(operation model.UpdateOperation) bool {
	return p.providerMaintenanceOperation != "" && p.providerMaintenanceOperation == operation.OperationID
}

func providerProofMatchesOperation(operation model.UpdateOperation, observed contract.ObservationV1) bool {
	proof := observed.ProviderMaintenance
	return observed.ProviderRevision == contract.ProviderRevision && observed.ProviderMaintenanceRevision == contract.ProviderMaintenanceRevision &&
		observed.ManagementReady && proof != nil && contract.ValidateProviderMaintenanceObservation(*proof) == nil &&
		proof.OperationID == operation.OperationID && proof.ManifestDigest == operation.ManifestDigest && proof.ArtifactSetDigest == operation.ArtifactSetDigest &&
		proof.TargetSequence == operation.Sequence && proof.TargetPanelVersion == operation.Version && proof.Input.BackupRef == operation.BackupRef
}

func (p *BrokerProvider) providerProof(ctx context.Context, operation model.UpdateOperation) (*contract.ProviderMaintenanceObservationV1, error) {
	observed, err := p.observeLiveRollback(ctx, operation)
	if err != nil {
		return nil, err
	}
	if observed.ProviderMaintenance == nil {
		return nil, nil
	}
	if observed.ProviderMaintenance.OperationID != operation.OperationID && !p.providerMaintenanceFor(operation) {
		// Completed root evidence can outlive its operation. It cannot make a
		// later ordinary Update inherit provider-only rollback behavior.
		return nil, nil
	}
	if !providerProofMatchesOperation(operation, observed) {
		return nil, ErrRecoveryRequired
	}
	p.requireProviderMaintenance(operation)
	return observed.ProviderMaintenance, nil
}

func (p *BrokerProvider) providerCheckpoint(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	proof, err := p.providerProof(ctx, operation)
	return proof != nil, err
}

// A live root checkpoint survives restoration or loss of the application's
// operation history. Its presence can require a hold, but cannot reconstruct
// database authority or mark a restored operation trusted.
func (p *BrokerProvider) pendingProviderMaintenance(ctx context.Context) (bool, error) {
	observed, err := p.observeLiveRollback(ctx, model.UpdateOperation{OperationID: "update-provider-startup"})
	if err != nil {
		return false, err
	}
	proof := observed.ProviderMaintenance
	if proof == nil {
		return false, nil
	}
	if observed.ProviderRevision != contract.ProviderRevision || observed.ProviderMaintenanceRevision != contract.ProviderMaintenanceRevision ||
		!observed.ManagementReady || contract.ValidateProviderMaintenanceObservation(*proof) != nil {
		return true, ErrRecoveryRequired
	}
	return proof.Phase != "COMMITTED" && proof.Phase != "ROLLBACK_VERIFIED", nil
}

func (p *BrokerProvider) AdmitProviderRequest(ctx context.Context, operation model.UpdateOperation, request ProviderTransitionRequest) error {
	proof, err := p.providerProof(ctx, operation)
	if err != nil || proof == nil || proof.TargetProvider.ID != request.TargetProvider || proof.ArtifactSetDigest != request.ExpectedArtifactSetDigest ||
		proof.Input.ConfigurationRevision != request.ExpectedConfigurationRevision {
		return ErrReleaseChanged
	}
	return nil
}

func (p *BrokerProvider) AdmitProviderRollback(ctx context.Context, operation model.UpdateOperation) error {
	proof, err := p.providerProof(ctx, operation)
	if err != nil || proof == nil || p.configuration == nil || proof.TargetPanelVersion != proof.Input.PreviousPanelVersion {
		return ErrCoreProviderMaintenance
	}
	snapshot, err := p.configuration.CaptureProviderConfiguration(ctx)
	defer snapshot.Clear()
	if err != nil || snapshot.ConfigurationRevision != proof.Input.ConfigurationRevision || snapshot.ConfigurationDigest != proof.Input.ConfigurationDigest {
		return ErrReleaseChanged
	}
	return nil
}

func (p *BrokerProvider) PreflightProvider(ctx context.Context, operation model.UpdateOperation, verified release.Verified, artifacts []release.Artifact,
	snapshot CoreConfigurationSnapshot, persist func(string) (model.UpdateOperation, error)) (PreflightResult, error) {
	if p.configuration == nil || persist == nil || len(artifacts) != 1 {
		return PreflightResult{}, ErrProviderUnavailable
	}
	identity, err := releaseIdentity(operation, verified, artifacts)
	if err != nil || identity.CoreProvider == (release.CoreProviderSource{}) || identity.Version != configidentity.GetVersion() {
		return PreflightResult{}, ErrCoreProviderMaintenance
	}
	observed, err := p.observeLiveRollback(ctx, operation)
	if err != nil || observed.ProviderMaintenanceRevision != contract.ProviderMaintenanceRevision {
		return PreflightResult{}, ErrProviderUnavailable
	}
	digest, err := runningPanelDigest()
	if err != nil {
		return PreflightResult{}, ErrCoreProviderUnverified
	}
	backup, err := p.prepareDatabaseBackup(ctx, operation)
	if err != nil {
		return PreflightResult{}, err
	}
	operation, err = persist(backup)
	if err != nil {
		return PreflightResult{}, err
	}
	input := contract.ProviderMaintenanceV1{ConfigurationDigest: snapshot.ConfigurationDigest, ConfigurationBytes: int64(len(snapshot.Configuration)),
		ConfigurationRevision: snapshot.ConfigurationRevision, BackupRef: backup, PreviousMaintenance: snapshot.Maintenance,
		PreviousProvider: snapshot.Capabilities.Provider.Source, PreviousExecutableDigest: digest, PreviousPanelVersion: configidentity.GetVersion(),
		PreviousCapabilityRevision: snapshot.CapabilityRevision, RuntimeGeneration: snapshot.RuntimeGeneration}
	if contract.ValidateProviderMaintenance(input) != nil {
		return PreflightResult{}, ErrCoreProviderMaintenance
	}
	p.requireProviderMaintenance(operation)
	request := contract.PrepareRequestV1{Release: identity, ExpectedBrokerCapability: operation.BrokerCapability,
		ExpectedManagementRevision: operation.DeploymentRevision, Maintenance: &input}
	var prepared contract.PrepareResultV1
	if err := p.mutate(ctx, broker.VerbUpdatePrepare, operation, 800_001, request, &prepared); err != nil {
		return PreflightResult{}, err
	}
	if !providerPreparedResult(operation, prepared, false) {
		return PreflightResult{}, ErrRecoveryRequired
	}
	if err := p.configuration.SetCoreMaintenance(ctx, snapshot.RuntimeGeneration, true); err != nil {
		return PreflightResult{}, err
	}
	for offset, index := 0, uint64(0); offset < len(snapshot.Configuration); index++ {
		end := min(offset+contract.MaxChunkBytes, len(snapshot.Configuration))
		var result contract.StageChunkResultV1
		chunk := contract.StageChunkRequestV1{Release: identity, Configuration: &input, Offset: int64(offset), Chunk: snapshot.Configuration[offset:end], Final: end == len(snapshot.Configuration)}
		if err := p.mutate(ctx, broker.VerbUpdateStage, operation, 810_001+index, chunk, &result); err != nil {
			return PreflightResult{}, err
		}
		if result.ProviderRevision != contract.ProviderRevision || result.AcceptedBytes != int64(end) || result.Complete != chunk.Final ||
			result.ArtifactDigest != "" || chunk.Final && result.ConfigurationDigest != snapshot.ConfigurationDigest {
			return PreflightResult{}, ErrRevisionMismatch
		}
		offset = end
	}
	request.ValidateConfiguration, request.MaintenanceHeld = true, true
	if err := p.mutate(ctx, broker.VerbUpdatePrepare, operation, 820_001, request, &prepared); err != nil {
		return PreflightResult{}, err
	}
	if !providerPreparedResult(operation, prepared, true) || prepared.ConfigurationRevision != snapshot.ConfigurationRevision {
		return PreflightResult{}, ErrRecoveryRequired
	}
	proof, err := p.providerProof(ctx, operation)
	if err != nil || proof == nil || proof.Phase != "VALIDATED" {
		return PreflightResult{}, ErrRecoveryRequired
	}
	return PreflightResult{RollbackAvailable: true, BackupRef: backup}, nil
}

func providerPreparedResult(operation model.UpdateOperation, result contract.PrepareResultV1, validated bool) bool {
	return result.ProviderRevision == contract.ProviderRevision && result.ManagementReady && result.RollbackAvailable &&
		result.PreparedRef == updateRef(operation, "prepared") && result.RollbackRef == updateRef(operation, "rollback") && result.ConfigurationVerified == validated
}

func runningPanelDigest() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	object, err := executableobject.Open(path, executableobject.Policy{MaxBytes: release.MaxPanelMemberBytes, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, RequiredMode: 0o755, ForbiddenMode: 0o022, RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		AllowRootOwnedStickyAncestry: true, RequireStablePath: true})
	if err != nil {
		return "", err
	}
	digest := object.Identity().Digest
	err = errors.Join(object.Revalidate(), object.Close())
	return digest, err
}

func (p *BrokerProvider) providerSnapshot(ctx context.Context, proof contract.ProviderMaintenanceObservationV1, held bool) (CoreConfigurationSnapshot, error) {
	if p.configuration == nil {
		return CoreConfigurationSnapshot{}, ErrProviderUnavailable
	}
	snapshot, err := p.configuration.CaptureProviderConfiguration(ctx)
	if err != nil || snapshot.ConfigurationRevision != proof.Input.ConfigurationRevision || snapshot.ConfigurationDigest != proof.Input.ConfigurationDigest ||
		snapshot.Maintenance != held || semanticBytesDigest(snapshot.Configuration) != snapshot.ConfigurationDigest {
		snapshot.Clear()
		return CoreConfigurationSnapshot{}, ErrReleaseChanged
	}
	return snapshot, nil
}

func (p *BrokerProvider) activateProvider(ctx context.Context, operation model.UpdateOperation, proof contract.ProviderMaintenanceObservationV1) error {
	if proof.Phase != "VALIDATED" {
		return ErrRevisionMismatch
	}
	snapshot, err := p.providerSnapshot(ctx, proof, true)
	defer snapshot.Clear()
	if err != nil {
		return err
	}
	var result contract.ActivateResultV1
	err = p.mutate(ctx, broker.VerbUpdateActivate, operation, 800_002, contract.ActivateRequestV1{Release: releaseIdentityFromOperation(operation),
		PreparedRef: updateRef(operation, "prepared"), RollbackRef: updateRef(operation, "rollback"), ExpectedMode: "native", MaintenanceHeld: true,
		ConfigurationDigest: proof.Input.ConfigurationDigest, ConfigurationRevision: proof.Input.ConfigurationRevision}, &result)
	if err != nil {
		return err
	}
	if result.ProviderRevision != contract.ProviderRevision || result.ActiveSequence != operation.Sequence || result.ActiveDigest != operation.ManifestDigest {
		return ErrRevisionMismatch
	}
	return nil
}

func (p *BrokerProvider) verifyProvider(ctx context.Context, operation model.UpdateOperation, proof contract.ProviderMaintenanceObservationV1, rollback bool) (bool, error) {
	if p.configuration == nil {
		return false, ErrProviderUnavailable
	}
	expectedSource, expectedDigest, expectedVersion, expectedCapabilities := proof.TargetProvider, proof.ExecutableDigest, proof.TargetPanelVersion, proof.CapabilityRevision
	if rollback {
		expectedSource, expectedDigest, expectedVersion, expectedCapabilities = proof.Input.PreviousProvider, proof.Input.PreviousExecutableDigest, proof.Input.PreviousPanelVersion, proof.Input.PreviousCapabilityRevision
	}
	current := coreprovider.Current()
	if !coreprovider.SourceIdentityVerified(current) || current.Source != expectedSource || configidentity.GetVersion() != expectedVersion {
		return false, ErrRestartPending
	}
	digest, err := runningPanelDigest()
	if err != nil || digest != expectedDigest {
		return false, ErrRestartPending
	}
	snapshot, err := p.configuration.CaptureProviderConfiguration(ctx)
	defer snapshot.Clear()
	if err != nil || snapshot.ConfigurationRevision != proof.Input.ConfigurationRevision || snapshot.ConfigurationDigest != proof.Input.ConfigurationDigest ||
		snapshot.CapabilityRevision != expectedCapabilities {
		return false, ErrReleaseChanged
	}
	if snapshot.Maintenance != proof.Input.PreviousMaintenance || !snapshot.Maintenance && snapshot.RuntimeState != "RUNNING" {
		if err := p.configuration.SetCoreMaintenance(ctx, snapshot.RuntimeGeneration, proof.Input.PreviousMaintenance); err != nil {
			return false, err
		}
	}
	healthSnapshot, err := p.providerSnapshot(ctx, proof, proof.Input.PreviousMaintenance)
	defer healthSnapshot.Clear()
	if err != nil {
		return false, err
	}
	health := contract.ProviderHealthV1{ConfigurationDigest: healthSnapshot.ConfigurationDigest, ConfigurationRevision: healthSnapshot.ConfigurationRevision,
		Maintenance: healthSnapshot.Maintenance, RuntimeState: healthSnapshot.RuntimeState, RuntimeGeneration: healthSnapshot.RuntimeGeneration, CapabilityRevision: healthSnapshot.CapabilityRevision}
	var result contract.VerifyResultV1
	ordinal := uint64(800_003)
	if rollback {
		ordinal = 800_005
	}
	err = p.mutate(ctx, broker.VerbUpdateVerify, operation, ordinal, contract.VerifyRequestV1{Release: releaseIdentityFromOperation(operation),
		PreparedRef: updateRef(operation, "prepared"), RollbackRef: updateRef(operation, "rollback"), ProviderHealth: &health, Rollback: rollback}, &result)
	if err != nil {
		return false, err
	}
	if !result.Verified && result.HealthRevision == "restart-pending" {
		return false, ErrRestartPending
	}
	if result.ProviderRevision != contract.ProviderRevision || !result.Verified || !result.ManagementReady {
		return false, ErrRecoveryRequired
	}
	if !rollback && (result.VerifiedSequence != operation.Sequence || result.VerifiedDigest != operation.ManifestDigest) {
		return false, ErrRevisionMismatch
	}
	return true, nil
}

func (p *BrokerProvider) rollbackProvider(ctx context.Context, operation model.UpdateOperation, proof contract.ProviderMaintenanceObservationV1) (bool, error) {
	if p.configuration == nil {
		return false, ErrProviderUnavailable
	}
	// Provider-only rollback preserves the live database, including accepted
	// observations. Panel version changes need the canonical restore lifecycle.
	if proof.TargetPanelVersion != proof.Input.PreviousPanelVersion {
		return false, ErrCoreProviderMaintenance
	}
	if proof.Phase != "ROLLED_BACK" && proof.Phase != "ROLLBACK_VERIFIED" {
		snapshot, err := p.configuration.CaptureProviderConfiguration(ctx)
		defer snapshot.Clear()
		if err != nil || snapshot.ConfigurationRevision != proof.Input.ConfigurationRevision || snapshot.ConfigurationDigest != proof.Input.ConfigurationDigest {
			return false, ErrReleaseChanged
		}
		if err := p.configuration.SetCoreMaintenance(ctx, snapshot.RuntimeGeneration, true); err != nil {
			return false, err
		}
		var result contract.RollbackResultV1
		if err := p.mutate(ctx, broker.VerbUpdateRollback, operation, 800_004, contract.RollbackRequestV1{Release: releaseIdentityFromOperation(operation),
			RollbackRef: updateRef(operation, "rollback"), ReasonCode: operation.ReasonCode}, &result); err != nil {
			return false, err
		}
		if result.ProviderRevision != contract.ProviderRevision || !result.RolledBack || !result.ManagementReady {
			return false, ErrRevisionMismatch
		}
	}
	return p.verifyProvider(ctx, operation, proof, true)
}
