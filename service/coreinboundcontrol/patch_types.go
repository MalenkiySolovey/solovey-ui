package coreinboundcontrol

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-host/coreruntime/inboundcontrol"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
	"time"
)

const (
	FallbackPatchPreviewSchemaV1 = canonical.FallbackPatchPreviewSchemaV1
	FallbackCheckpointSchemaV1   = canonical.FallbackCheckpointSchemaV1
	FallbackMutationSchemaV1     = canonical.FallbackMutationSchemaV1
)

type FallbackPatchVariantV1 = canonical.FallbackPatchVariantV1

const (
	FallbackPatchVLESSRealityHandshakeTCP = canonical.FallbackPatchVLESSRealityHandshakeTCP
	FallbackPatchTrojanDefaultTCP         = canonical.FallbackPatchTrojanDefaultTCP
	FallbackPatchTrojanALPNTCP            = canonical.FallbackPatchTrojanALPNTCP
)

type ApprovedEndpointV1 = canonical.ApprovedEndpointV1

type FallbackPatchExpectationsV1 = canonical.FallbackPatchExpectationsV1

type PreviewFallbackPatchRequestV1 = canonical.PreviewFallbackPatchRequestV1

type ChangedFieldV1 = canonical.ChangedFieldV1

type FallbackPatchPreviewV1 = canonical.FallbackPatchPreviewV1

type CheckpointPreparationV1 = canonical.CheckpointPreparationV1

type CheckpointStateV1 = canonical.CheckpointStateV1

const (
	CheckpointStatePrepared          = canonical.CheckpointStatePrepared
	CheckpointStateCommitted         = canonical.CheckpointStateCommitted
	CheckpointStateRuntimeApplied    = canonical.CheckpointStateRuntimeApplied
	CheckpointStateEffectiveVerified = canonical.CheckpointStateEffectiveVerified
	CheckpointStateRestoredCommitted = canonical.CheckpointStateRestoredCommitted
	CheckpointStateRestoreFailed     = canonical.CheckpointStateRestoreFailed
	CheckpointStateRestoredVerified  = canonical.CheckpointStateRestoredVerified
	CheckpointStateReleased          = canonical.CheckpointStateReleased
)

type InspectCheckpointRequestV1 = canonical.InspectCheckpointRequestV1

// FindCheckpointRequestV1 is intentionally limited to the core-generated
// preview digest. It exists only to adopt a checkpoint whose identifier could
// not be journaled after a process interruption.
type FindCheckpointRequestV1 = canonical.FindCheckpointRequestV1

// CheckpointStatusV1 exposes only integrity and revision facts needed by an
// owning recovery workflow. It never returns the secret-sensitive checkpoint
// payload or the previous inbound configuration.
type CheckpointStatusV1 = canonical.CheckpointStatusV1

type PrepareCheckpointRequestV1 = canonical.PrepareCheckpointRequestV1

type ApplyFallbackPatchRequestV1 = canonical.ApplyFallbackPatchRequestV1

type RuntimeInboundObservationV1 = canonical.RuntimeInboundObservationV1

type FallbackMutationResultV1 = canonical.FallbackMutationResultV1

type VerifyEffectiveRequestV1 = canonical.VerifyEffectiveRequestV1

type EffectiveVerificationV1 = canonical.EffectiveVerificationV1

type RestoreCheckpointRequestV1 = canonical.RestoreCheckpointRequestV1

type RestoreCheckpointResultV1 = canonical.RestoreCheckpointResultV1

type CheckpointTerminalProofKind = canonical.CheckpointTerminalProofKind

const (
	CheckpointProofApplyNeverCommitted = canonical.CheckpointProofApplyNeverCommitted
	CheckpointProofRestoreVerified     = canonical.CheckpointProofRestoreVerified
	CheckpointProofDurablyAdopted      = canonical.CheckpointProofDurablyAdopted
)

type ReleaseCheckpointRequestV1 = canonical.ReleaseCheckpointRequestV1

type CheckpointReleaseV1 = canonical.CheckpointReleaseV1

type AdapterErrorCode = canonical.AdapterErrorCode

const (
	ErrorUnsupportedRuntime  = canonical.ErrorUnsupportedRuntime
	ErrorUnsupportedConfig   = canonical.ErrorUnsupportedConfig
	ErrorStalePreview        = canonical.ErrorStalePreview
	ErrorStaleBeforeRevision = canonical.ErrorStaleBeforeRevision
	ErrorSharedTLS           = canonical.ErrorSharedTLS
	ErrorInvalidEndpoint     = canonical.ErrorInvalidEndpoint
	ErrorInvalidCandidate    = canonical.ErrorInvalidCandidate
	ErrorCheckpointMissing   = canonical.ErrorCheckpointMissing
	ErrorCheckpointTampered  = canonical.ErrorCheckpointTampered
	ErrorCheckpointStale     = canonical.ErrorCheckpointStale
	ErrorMutationConflict    = canonical.ErrorMutationConflict
	ErrorDatabase            = canonical.ErrorDatabase
	ErrorRuntimeApply        = canonical.ErrorRuntimeApply
	ErrorEffectiveVerify     = canonical.ErrorEffectiveVerify
	ErrorRestoreDrift        = canonical.ErrorRestoreDrift
	ErrorRestoreFailure      = canonical.ErrorRestoreFailure
	ErrorReconcileRequired   = canonical.ErrorReconcileRequired
	ErrorCancelled           = canonical.ErrorCancelled
	ErrorAmbiguousResult     = canonical.ErrorAmbiguousResult
	ErrorCheckpointRelease   = canonical.ErrorCheckpointRelease
)

type AdapterError = canonical.AdapterError

func IsAdapterError(err error, code AdapterErrorCode) bool {
	return canonical.IsAdapterError(err, code)
}

type MutationCoordinator interface {
	RunBlockingContext(context.Context, func() error) error
}

type RuntimeMutationController interface {
	ApplyInbound(context.Context, uint) (RuntimeInboundObservationV1, error)
	ObserveInbound(context.Context, string) (RuntimeInboundObservationV1, error)
}

type MutationHooks interface {
	BeforeCommit(context.Context, *gorm.DB, *model.Inbound, FallbackPatchVariantV1, []ChangedFieldV1) error
	AfterCommit(FallbackPatchVariantV1, uint)
}

type CandidateHydrator interface {
	HydrateInbound(context.Context, *gorm.DB, *model.Inbound, []byte) ([]byte, error)
}

type candidateValidator interface {
	ValidateInbound(context.Context, []byte) error
}

type MutationDependencies struct {
	Coordinator MutationCoordinator
	Runtime     RuntimeMutationController
	Hooks       MutationHooks
	Hydrator    CandidateHydrator
	validator   candidateValidator
	now         func() time.Time
}
