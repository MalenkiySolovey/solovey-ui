package coreinboundcontrol

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const (
	FallbackPatchPreviewSchemaV1 = "solovey-ui/inbound-fallback-patch-preview/v1"
	FallbackCheckpointSchemaV1   = "solovey-ui/inbound-fallback-checkpoint/v1"
	FallbackMutationSchemaV1     = "solovey-ui/inbound-fallback-mutation/v1"
)

type FallbackPatchVariantV1 string

const (
	FallbackPatchVLESSRealityHandshakeTCP FallbackPatchVariantV1 = "VLESS_REALITY_HANDSHAKE_TCP"
	FallbackPatchTrojanDefaultTCP         FallbackPatchVariantV1 = "TROJAN_DEFAULT_FALLBACK_TCP"
	FallbackPatchTrojanALPNTCP            FallbackPatchVariantV1 = "TROJAN_ALPN_FALLBACK_TCP"
)

type ApprovedEndpointV1 struct {
	ProviderID           string   `json:"providerId"`
	EndpointID           string   `json:"endpointId"`
	EndpointRevision     string   `json:"endpointRevision"`
	Network              string   `json:"network"`
	AddressFamily        string   `json:"addressFamily"`
	Bind                 string   `json:"bind"`
	Port                 uint16   `json:"port"`
	Local                bool     `json:"local"`
	TransportSecurity    string   `json:"transportSecurity"`
	ApplicationProtocols []string `json:"applicationProtocols,omitempty"`
}

type FallbackPatchExpectationsV1 struct {
	InboundDatabaseID          uint   `json:"inboundDatabaseId"`
	ResourceID                 string `json:"resourceId"`
	ConfigurationRevision      string `json:"configurationRevision"`
	RuntimeIdentityRevision    string `json:"runtimeIdentityRevision"`
	CapabilityResolverRevision string `json:"capabilityResolverRevision"`
	EndpointRevision           string `json:"endpointRevision"`
}

type PreviewFallbackPatchRequestV1 struct {
	Expected          FallbackPatchExpectationsV1 `json:"expected"`
	Variant           FallbackPatchVariantV1      `json:"variant"`
	ApprovedEndpoint  ApprovedEndpointV1          `json:"approvedEndpoint"`
	ReplaceDefaultToo bool                        `json:"replaceDefaultToo,omitempty"`
}

type ChangedFieldV1 struct {
	Path string `json:"path"`
}

type FallbackPatchPreviewV1 struct {
	Schema                      string                 `json:"schema"`
	PreviewID                   string                 `json:"previewId"`
	Digest                      string                 `json:"digest"`
	InboundDatabaseID           uint                   `json:"inboundDatabaseId"`
	ResourceID                  string                 `json:"resourceId"`
	Variant                     FallbackPatchVariantV1 `json:"variant"`
	BeforeConfigurationRevision string                 `json:"beforeConfigurationRevision"`
	ExpectedAfterRevision       string                 `json:"expectedAfterRevision"`
	RuntimeIdentityRevision     string                 `json:"runtimeIdentityRevision"`
	CapabilityResolverRevision  string                 `json:"capabilityResolverRevision"`
	EndpointProviderID          string                 `json:"endpointProviderId"`
	EndpointID                  string                 `json:"endpointId"`
	EndpointRevision            string                 `json:"endpointRevision"`
	ChangedFields               []ChangedFieldV1       `json:"changedFields"`
	WarningCodes                []ReasonCode           `json:"warningCodes,omitempty"`
	ExpiresAt                   time.Time              `json:"expiresAt"`
}

type CheckpointPreparationV1 struct {
	Schema                  string    `json:"schema"`
	CheckpointID            string    `json:"checkpointId"`
	PreviewDigest           string    `json:"previewDigest"`
	IntegrityDigest         string    `json:"integrityDigest"`
	UncommittedReleaseProof string    `json:"uncommittedReleaseProof"`
	ExpiresAt               time.Time `json:"expiresAt"`
}

type CheckpointStateV1 string

const (
	CheckpointStatePrepared          CheckpointStateV1 = "PREPARED"
	CheckpointStateCommitted         CheckpointStateV1 = "COMMITTED"
	CheckpointStateRuntimeApplied    CheckpointStateV1 = "RUNTIME_APPLIED"
	CheckpointStateEffectiveVerified CheckpointStateV1 = "EFFECTIVE_VERIFIED"
	CheckpointStateRestoredCommitted CheckpointStateV1 = "RESTORED_COMMITTED"
	CheckpointStateRestoreFailed     CheckpointStateV1 = "RESTORE_FAILED"
	CheckpointStateRestoredVerified  CheckpointStateV1 = "RESTORED_VERIFIED"
	CheckpointStateReleased          CheckpointStateV1 = "RELEASED"
)

type InspectCheckpointRequestV1 struct {
	CheckpointID string `json:"checkpointId"`
}

// FindCheckpointRequestV1 is intentionally limited to the core-generated
// preview digest. It exists only to adopt a checkpoint whose identifier could
// not be journaled after a process interruption.
type FindCheckpointRequestV1 struct {
	PreviewDigest string `json:"previewDigest"`
}

// CheckpointStatusV1 exposes only integrity and revision facts needed by an
// owning recovery workflow. It never returns the secret-sensitive checkpoint
// payload or the previous inbound configuration.
type CheckpointStatusV1 struct {
	CheckpointID                 string            `json:"checkpointId"`
	State                        CheckpointStateV1 `json:"state"`
	PreviewDigest                string            `json:"previewDigest"`
	IntegrityDigest              string            `json:"integrityDigest"`
	BeforeConfigurationRevision  string            `json:"beforeConfigurationRevision"`
	ExpectedAfterRevision        string            `json:"expectedAfterRevision"`
	CurrentConfigurationRevision string            `json:"currentConfigurationRevision"`
	CurrentEffectiveRevision     string            `json:"currentEffectiveRevision,omitempty"`
	ProofDigest                  string            `json:"proofDigest,omitempty"`
	DetachedReleaseProof         string            `json:"detachedReleaseProof,omitempty"`
	UncommittedReleaseProof      string            `json:"uncommittedReleaseProof,omitempty"`
}

type PrepareCheckpointRequestV1 struct {
	Preview           FallbackPatchPreviewV1 `json:"preview"`
	ApprovedEndpoint  ApprovedEndpointV1     `json:"approvedEndpoint"`
	ReplaceDefaultToo bool                   `json:"replaceDefaultToo,omitempty"`
}

type ApplyFallbackPatchRequestV1 struct {
	CheckpointID           string             `json:"checkpointId"`
	ExpectedBeforeRevision string             `json:"expectedBeforeRevision"`
	ApprovedEndpoint       ApprovedEndpointV1 `json:"approvedEndpoint"`
}

type RuntimeInboundObservationV1 struct {
	RuntimeAvailable     bool   `json:"runtimeAvailable"`
	Tag                  string `json:"tag,omitempty"`
	Type                 string `json:"type,omitempty"`
	OptionsDigest        string `json:"optionsDigest,omitempty"`
	ManagerGeneration    uint64 `json:"managerGeneration,omitempty"`
	MatchingInboundCount int    `json:"matchingInboundCount"`
}

type FallbackMutationResultV1 struct {
	Schema                      string                      `json:"schema"`
	CheckpointID                string                      `json:"checkpointId"`
	InboundDatabaseID           uint                        `json:"inboundDatabaseId"`
	BeforeConfigurationRevision string                      `json:"beforeConfigurationRevision"`
	AfterConfigurationRevision  string                      `json:"afterConfigurationRevision"`
	ExpectedEffectiveRevision   string                      `json:"expectedEffectiveRevision"`
	Observation                 RuntimeInboundObservationV1 `json:"observation"`
	AlreadyCommitted            bool                        `json:"alreadyCommitted"`
}

type VerifyEffectiveRequestV1 struct {
	CheckpointID              string `json:"checkpointId"`
	ExpectedAfterRevision     string `json:"expectedAfterRevision"`
	ExpectedEffectiveRevision string `json:"expectedEffectiveRevision"`
}

type EffectiveVerificationV1 struct {
	CheckpointID          string                      `json:"checkpointId"`
	ConfigurationRevision string                      `json:"configurationRevision"`
	EffectiveRevision     string                      `json:"effectiveRevision"`
	Verified              bool                        `json:"verified"`
	ProofDigest           string                      `json:"proofDigest"`
	Observation           RuntimeInboundObservationV1 `json:"observation"`
}

type RestoreCheckpointRequestV1 struct {
	CheckpointID            string `json:"checkpointId"`
	ExpectedCurrentRevision string `json:"expectedCurrentRevision"`
}

type RestoreCheckpointResultV1 struct {
	CheckpointID                  string `json:"checkpointId"`
	RestoredConfigurationRevision string `json:"restoredConfigurationRevision"`
	RestoredEffectiveRevision     string `json:"restoredEffectiveRevision"`
	ProofDigest                   string `json:"proofDigest"`
}

type CheckpointTerminalProofKind string

const (
	CheckpointProofApplyNeverCommitted CheckpointTerminalProofKind = "apply_never_committed"
	CheckpointProofRestoreVerified     CheckpointTerminalProofKind = "restore_verified"
	CheckpointProofDurablyAdopted      CheckpointTerminalProofKind = "durably_adopted"
)

type ReleaseCheckpointRequestV1 struct {
	CheckpointID string                      `json:"checkpointId"`
	Kind         CheckpointTerminalProofKind `json:"kind"`
	ProofDigest  string                      `json:"proofDigest"`
}

type CheckpointReleaseV1 struct {
	CheckpointID string    `json:"checkpointId"`
	ReleasedAt   time.Time `json:"releasedAt"`
}

type AdapterErrorCode string

const (
	ErrorUnsupportedRuntime  AdapterErrorCode = "unsupported_runtime"
	ErrorUnsupportedConfig   AdapterErrorCode = "unsupported_configuration"
	ErrorStalePreview        AdapterErrorCode = "stale_preview"
	ErrorStaleBeforeRevision AdapterErrorCode = "stale_before_revision"
	ErrorSharedTLS           AdapterErrorCode = "shared_tls_row"
	ErrorInvalidEndpoint     AdapterErrorCode = "invalid_target_endpoint"
	ErrorInvalidCandidate    AdapterErrorCode = "invalid_candidate"
	ErrorCheckpointMissing   AdapterErrorCode = "checkpoint_missing"
	ErrorCheckpointTampered  AdapterErrorCode = "checkpoint_tampered"
	ErrorCheckpointStale     AdapterErrorCode = "checkpoint_stale"
	ErrorMutationConflict    AdapterErrorCode = "mutation_conflict"
	ErrorDatabase            AdapterErrorCode = "database_failure"
	ErrorRuntimeApply        AdapterErrorCode = "runtime_apply_failure"
	ErrorEffectiveVerify     AdapterErrorCode = "effective_verify_failure"
	ErrorRestoreDrift        AdapterErrorCode = "restore_drift"
	ErrorRestoreFailure      AdapterErrorCode = "restore_failure"
	ErrorReconcileRequired   AdapterErrorCode = "reconcile_required"
	ErrorCancelled           AdapterErrorCode = "cancelled"
	ErrorAmbiguousResult     AdapterErrorCode = "ambiguous_result"
	ErrorCheckpointRelease   AdapterErrorCode = "checkpoint_release_rejected"
)

type AdapterError struct {
	Code AdapterErrorCode
}

func (e *AdapterError) Error() string {
	if e == nil {
		return "core fallback adapter failure"
	}
	return string(e.Code)
}

func IsAdapterError(err error, code AdapterErrorCode) bool {
	typed, ok := err.(*AdapterError)
	return ok && typed.Code == code
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
