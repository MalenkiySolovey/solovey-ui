// Package durable defines typed protection records and journal transitions.
// Database models, table metadata, transactions and connection admission remain
// with the Backend adapter. Historical Model names preserve response shapes.
package durable

import (
	"encoding/json"
	"errors"
	neutralfallback "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"strings"
	"time"
)

type AcquireOperationLockInput struct {
	OperationID        string
	Kind               string
	ResourceID         string
	Protocol           string
	Listen             string
	Port               *int
	State              string
	IdempotencyKey     string
	PlanRevision       string
	HelperRevision     string
	LockedByPID        int
	LockedByInstanceID string
	Actor              string
	Now                int64
	ExpiresAt          int64
}

type ArtifactModel struct {
	ID             uint
	OperationID    string
	Revision       string
	Scope          string
	RelativePath   string
	ManifestSHA256 string
	Bytes          int64
	CreatedAt      int64
	UpdatedAt      int64
}

type FallbackTargetLeaseModel struct {
	ID                          uint
	Schema                      string
	LeaseID                     string
	HolderID                    string
	StrategyPlanID              string
	DecisionID                  string
	ActionID                    string
	OperationID                 string
	ResourceID                  string
	ProviderReservationID       string
	ProviderReservationRevision string
	ProviderID                  string
	TargetID                    string
	PublishRevision             string
	ContentDigest               string
	ApprovedLocalEndpointID     string
	EndpointRevision            string
	ProviderHealthRevision      string
	CapacityRevision            string
	ProviderRevision            string
	IssuedAt                    int64
	RenewedAt                   int64
	ExpiresAt                   int64
	ReleasedAt                  int64
	State                       string
	ReasonCodesJSON             json.RawMessage
}

type FencedOperationLockUpdate struct {
	OperationID    string
	Revision       int
	InstanceID     string
	PID            int
	FromStates     []string
	ToState        string
	HelperRevision *string
	Now            int64
	ExpiresAt      int64
}

type FirewallAuthoritySnapshot struct {
	Contributions  []FirewallContributionModel
	Composition    FirewallCompositionModel
	HasComposition bool
	Observation    FirewallObservationModel
	HasObservation bool
}

type FirewallCompositionModel struct {
	Runtime                        FirewallRuntimeBinding
	ID                             uint
	Schema                         string
	Revision                       string
	ManagedPlanRevision            string
	CandidateSHA256                string
	CandidateSemanticSHA256        string
	CandidateTimedMembershipSHA256 string
	BindingsJSON                   json.RawMessage
	State                          string
	AppliedOperationID             string
	UpdatedAt                      int64
}

type FirewallContributionModel struct {
	ContributionID     string
	Schema             string
	Kind               string
	ResourceID         string
	EndpointID         string
	Network            string
	AddressFamily      string
	SemanticRevision   string
	SemanticJSON       json.RawMessage
	AppliedOperationID string
	CreatedAt          int64
	UpdatedAt          int64
}

type FirewallContributionTransitionModel struct {
	RuntimeRetirementReason        string
	OperationID                    string
	Schema                         string
	ContributionID                 string
	PreviousPresent                bool
	PreviousSemanticRevision       string
	PreviousJSON                   json.RawMessage
	DesiredSemanticRevision        string
	DesiredJSON                    json.RawMessage
	BeforeCompositionRevision      string
	AfterCompositionRevision       string
	ManagedPlanRevision            string
	CandidateSHA256                string
	CandidateSemanticSHA256        string
	CandidateTimedMembershipSHA256 string
	State                          string
	MarkerUnixNano                 int64
	MutationCompletedUnixNano      int64
	HealthProviderInstance         string
	HealthGeneration               uint64
	HealthObservationRevision      string
	HealthStartedUnixNano          int64
	HealthCompletedUnixNano        int64
	HealthExpiresUnixNano          int64
	CreatedAt                      int64
	UpdatedAt                      int64
}

type FirewallHistoryPruneResult struct {
	DeletedOperations  int
	DeletedTransitions int
	DeletedBytes       int64
	Preserved          int
}

type FirewallObservationModel struct {
	ID                            uint
	Schema                        string
	State                         string
	HasCommittedAuthority         bool
	CommittedCompositionRevision  string
	ManagedTablePresent           bool
	CurrentRevision               string
	CurrentSemanticSHA256         string
	CurrentTimedMembershipSHA256  string
	ExpectedTimedMembershipSHA256 string
	Reason                        string
	ObservedAt                    int64
	UpdatedAt                     int64
}

type FirewallRuntimeBinding struct {
	Schema                   string
	VerifiedBoot             string
	AttemptBoot              string
	State                    string
	Reason                   string
	StartedAt                int64
	Deadline                 int64
	MutationAt               int64
	HealthAt                 int64
	HealthRevision           string
	ArtifactRevision         string
	ArtifactSHA256           string
	ArtifactMembershipSHA256 string
	CleanupAttempted         bool
}

type FrontingIdempotencyV2Model struct {
	ID                uint
	Action            string
	IdempotencyKey    string
	RequestDigest     string
	OperationID       string
	OperationRevision int
	Status            string
	ResponseJSON      json.RawMessage
	CreatedAt         int64
	UpdatedAt         int64
}

type FrontingStateV2Model struct {
	ResourceID                 string
	Schema                     string
	DisplayIdentity            string
	DesiredStrategy            string
	SelectedStrategy           string
	ActualState                string
	ApplyGate                  string
	RuntimeState               string
	InstallationClass          string
	RuntimeIdentityRevision    string
	StrategyCapabilityRevision string
	SocketClaimJSON            json.RawMessage
	BackendReferencesJSON      json.RawMessage
	FallbackReferencesJSON     json.RawMessage
	SelectorSetJSON            json.RawMessage
	LeaseMirrorsJSON           json.RawMessage
	DefaultPolicy              string
	SelectedProxyMode          string
	ActiveMapRevision          string
	CandidateRevision          string
	ActiveRevision             string
	LatestOperationID          string
	LatestOperationRevision    int
	LatestOperationState       string
	HealthState                string
	HealthObservedAt           int64
	HealthExpiresAt            int64
	RecoveryClassification     string
	CompatibilityState         string
	ReasonCodesJSON            json.RawMessage
	BlocksJSON                 json.RawMessage
	WarningsJSON               json.RawMessage
	SafeNextAction             string
	GuardingProviderLease      bool
	RecoverableArtifact        bool
	OwnsActiveManagedRevision  bool
	CreatedAt                  int64
	UpdatedAt                  int64
}

type GraylistModel struct {
	ID         uint
	ProfileID  *uint
	ResourceID string
	IPCIDR     string
	IPFamily   int
	Score      int
	Reason     string
	LastSignal string
	ExpiresAt  int64
	CreatedAt  int64
	UpdatedAt  int64
}

type IPAllowlistModel struct {
	ID                     uint
	IPCIDR                 string
	Reason                 string
	ExpiresAt              *int64
	BroadScopeAcknowledged bool
	CreatedBy              string
	CreatedAt              int64
	UpdatedAt              int64
}

type IdempotencyReceiptPruneResult struct {
	DeletedFrontingReceipts   int
	DeletedUDPGuardReceipts   int
	DeletedLocalProxyReceipts int
	DeletedBytes              int64
	Preserved                 int
}

type LocalProxyIdempotencyV1Model struct {
	ID                   uint
	Action               string
	IdempotencyKey       string
	RequestDigest        string
	OperationID          string
	OperationRevision    int
	Status               string
	SemanticResponseJSON json.RawMessage
	CreatedAt            int64
	UpdatedAt            int64
}

type LocalProxyStateV1Model struct {
	ID                      uint
	ResourceID              string
	EndpointID              string
	Schema                  string
	ActualState             string
	ApplyGate               string
	PlanID                  string
	PlanDigest              string
	PlanJSON                json.RawMessage
	FactRevision            string
	ReferenceRevision       string
	LeaseID                 string
	LeaseRevision           string
	LeaseState              string
	LeaseRenewedAt          int64
	LeaseExpiresAt          int64
	LatestOperationID       string
	LatestOperationRevision int
	MarkerRevision          string
	HealthJSON              json.RawMessage
	HealthRevision          string
	HealthExpiresUnixNano   int64
	GuardingProviderLease   bool
	RecoveryRequired        bool
	CreatedAt               int64
	UpdatedAt               int64
}

type NativeFallbackHistoryPruneResult struct {
	DeletedOperations   int
	DeletedLocks        int
	DeletedReservations int
	DeletedBytes        int64
	Preserved           int
}

type NativeFallbackJournalUpdate struct {
	OperationID                string
	ExpectedRevision           int
	Stage                      NativeJournalStage
	Reservation                *neutralfallback.ProviderTargetReservationV1
	CheckpointID               string
	CheckpointDigest           string
	CheckpointReleaseProof     string
	CheckpointReleased         bool
	ArtifactRevision           string
	ArtifactManifestDigest     string
	AfterConfigurationRevision string
	ExpectedEffectiveRevision  string
	EffectiveRevision          string
	ManagerGeneration          uint64
	HealthResultRevision       string
	HealthFactsJSON            json.RawMessage
	RecoveryClassification     string
	ReasonCodes                []domain.NativeFallbackReasonCode
	RecoveryBundleJSON         json.RawMessage
	Now                        time.Time
}

type NativeFallbackOperationModel struct {
	ID                          uint
	Schema                      string
	OperationID                 string
	Revision                    int
	ResourceID                  string
	InboundDatabaseID           uint
	PlanID                      string
	PlanDigest                  string
	PlanJSON                    json.RawMessage
	RuntimeIdentityRevision     string
	CapabilityResolverRevision  string
	BeforeConfigurationRevision string
	ExpectedAfterRevision       string
	AfterConfigurationRevision  string
	BeforeEffectiveRevision     string
	ExpectedEffectiveRevision   string
	EffectiveRevision           string
	TargetReferenceJSON         json.RawMessage
	TargetRevision              string
	ProviderRevision            string
	EndpointRevision            string
	PublishRevision             string
	HealthRevision              string
	CapacityRevision            string
	ProviderReservationID       string
	ProviderReservationRevision string
	CoreCheckpointID            string
	CoreCheckpointDigest        string
	CheckpointReleaseProof      string
	CoreCheckpointReleasedAt    *int64
	ArtifactRevision            string
	ArtifactManifestDigest      string
	MutationMarkedAt            *int64
	WorkflowState               string
	HealthResultRevision        string
	HealthFactsJSON             json.RawMessage
	ManagerGeneration           uint64
	RollbackAttemptCount        int
	RecoveryClassification      string
	ReasonCodesJSON             json.RawMessage
	RecoveryBundleJSON          json.RawMessage
	CreatedAt                   int64
	UpdatedAt                   int64
	PreparedAt                  *int64
	AppliedAt                   *int64
	RolledBackAt                *int64
}

type NativeJournalStage string

type OperationLockModel struct {
	ID                 uint
	OperationID        string
	Kind               string
	ResourceID         string
	Protocol           string
	Listen             string
	Port               *int
	State              string
	Revision           int
	IdempotencyKey     string
	PlanRevision       string
	HelperRevision     string
	LockedByPID        *int
	LockedByInstanceID string
	Actor              string
	RecoveryAttempts   int
	LastRecoveryAt     *int64
	RecoveryErrorCode  string
	HeartbeatAt        int64
	ExpiresAt          int64
	CreatedAt          int64
	UpdatedAt          int64
}

type PageQuery struct {
	Page  int
	Limit int
}

type PortAllowlistModel struct {
	ID        uint
	Protocol  string
	Listen    string
	PortStart int
	PortEnd   int
	Reason    string
	ExpiresAt *int64
	CreatedBy string
	CreatedAt int64
	UpdatedAt int64
}

type ReclaimOperationLockUpdate struct {
	OperationID    string
	Revision       int
	InstanceID     string
	PID            int
	FromState      string
	ToState        string
	HelperRevision *string
	Now            int64
	ExpiresAt      int64
	RequireIdle    bool
}

type RecoveryOperationLockUpdate struct {
	OperationID      string
	ExpectedRevision int
	FromStates       []string
	ToState          string
	Now              int64
}

type UDPGuardIdempotencyV1Model struct {
	ID                   uint
	Action               string
	IdempotencyKey       string
	RequestDigest        string
	OperationID          string
	OperationRevision    int
	Status               string
	SemanticResponseJSON json.RawMessage
	CreatedAt            int64
	UpdatedAt            int64
}

type UDPGuardStateV1Model struct {
	ID                        uint
	ResourceID                string
	EndpointID                string
	AddressFamily             string
	Schema                    string
	DesiredPolicy             string
	SelectedStrategy          string
	ActualState               string
	PlanID                    string
	PlanDigest                string
	CapabilityRevision        string
	ClaimRevision             string
	PolicyRevision            string
	ContributionID            string
	ContributionRevision      string
	CompositionRevision       string
	ManagedPlanRevision       string
	HealthProviderInstance    string
	HealthGeneration          uint64
	HealthObservationRevision string
	HealthStartedUnixNano     int64
	HealthCompletedUnixNano   int64
	HealthExpiresUnixNano     int64
	LatestOperationID         string
	LatestOperationRevision   int
	RecoveryRequired          bool
	OwnsActiveContribution    bool
	RecoverableArtifact       bool
	CreatedAt                 int64
	UpdatedAt                 int64
}

var ErrIdempotencyKeyExpired = errors.New("idempotency key is outside the retained replay horizon; use a new key")
var ErrIdempotencyKeyInvalid = errors.New("structured idempotency key is invalid")
var ErrOperationConflict = errors.New("another server-protection system operation is active")
var ErrOperationFenced = errors.New("server-protection operation ownership changed")
var ErrRecordNotFound = errors.New("server-protection record not found")
var ErrRevisionConflict = errors.New("server-protection revision conflict")

const FirewallRuntimeSchema = "solovey-ui/firewall-runtime-continuation/v1"
const FrontingCompatibilityCurrentV2 = "CURRENT_V2"
const FrontingCompatibilityLegacyRepreview = "LEGACY_V1_REPREVIEW_REQUIRED"
const FrontingReceiptComplete = "COMPLETE"
const FrontingReceiptPending = "PENDING"
const FrontingStateSchemaV2 = "solovey-ui/fronting-semantic-state/v2"
const NativeFallbackMirrorSchemaV1 = "solovey-ui/provider-reservation-mirror/v1"
const NativeFallbackOperationSchemaV1 = "solovey-ui/native-fallback-operation/v1"
const NativeJournalApplied NativeJournalStage = "applied"
const NativeJournalApplying NativeJournalStage = "applying"
const NativeJournalCancelled NativeJournalStage = "cancelled"
const NativeJournalFenced NativeJournalStage = "fenced"
const NativeJournalHealth NativeJournalStage = "health"
const NativeJournalPrepared NativeJournalStage = "prepared"
const NativeJournalReconcileRequired NativeJournalStage = "reconcile_required"
const NativeJournalReservation NativeJournalStage = "reservation"
const NativeJournalReverified NativeJournalStage = "reverified"
const NativeJournalRollbackFailed NativeJournalStage = "rollback_failed"
const NativeJournalRolledBack NativeJournalStage = "rolled_back"
const NativeJournalRollingBack NativeJournalStage = "rolling_back"
const NativeRecoveryRestoredUntrusted = "restored_state_untrusted"
const NativeWorkflowApplied = "APPLIED"
const NativeWorkflowApplying = "APPLYING"
const NativeWorkflowCancelled = "CANCELLED"
const NativeWorkflowHealth = "HEALTH"
const NativeWorkflowPrepared = "PREPARED"
const NativeWorkflowPreparing = "PREPARING"
const NativeWorkflowReconcileRequired = "RECONCILE_REQUIRED"
const NativeWorkflowRollbackFailed = "ROLLBACK_FAILED"
const NativeWorkflowRolledBack = "ROLLED_BACK"
const NativeWorkflowRollingBack = "ROLLING_BACK"
const FrontingReceiptAmbiguous = "AMBIGUOUS"
const FirewallObservationSchemaV1 = "solovey-ui/managed-firewall-observation/v1"

const MaxFrontingSemanticBlobV2 = 64 << 10

func NonTerminalOperationLockStates() []string {
	return []string{"prepared", "applying", "health", "health_failed", "rolling_back", "lock_suspect", "restoring_runtime"}
}

func ValidFrontingStateV2Model(value FrontingStateV2Model) bool {
	if value.ResourceID == "" || len(value.ResourceID) > 256 || value.Schema != FrontingStateSchemaV2 ||
		!frontingStateDesiredV2(value.DesiredStrategy) || !frontingStateSelectedV2(value.SelectedStrategy) || !frontingStateActualV2(value.ActualState) ||
		value.ApplyGate != "EXPERIMENTAL_DISABLED_BY_DEFAULT" || !frontingRuntimeStateV2(value.RuntimeState) || !frontingInstallationClassV2(value.InstallationClass) ||
		(value.CompatibilityState != FrontingCompatibilityCurrentV2 && value.CompatibilityState != FrontingCompatibilityLegacyRepreview) ||
		value.CreatedAt <= 0 || value.UpdatedAt <= 0 || value.UpdatedAt < value.CreatedAt {
		return false
	}
	for _, data := range []json.RawMessage{value.SocketClaimJSON, value.BackendReferencesJSON, value.FallbackReferencesJSON, value.SelectorSetJSON, value.LeaseMirrorsJSON, value.ReasonCodesJSON, value.BlocksJSON, value.WarningsJSON} {
		if len(data) == 0 || len(data) > MaxFrontingSemanticBlobV2 || !json.Valid(data) {
			return false
		}
	}
	return true
}

func ValidFrontingIdempotencyV2Model(value FrontingIdempotencyV2Model) bool {
	if (value.Action != "apply" && value.Action != "rollback") || !frontingOpaqueV2(value.IdempotencyKey, 128) || !frontingDigestV2(value.RequestDigest) ||
		(value.Status != FrontingReceiptPending && value.Status != FrontingReceiptComplete && value.Status != FrontingReceiptAmbiguous) ||
		len(value.ResponseJSON) == 0 || len(value.ResponseJSON) > MaxFrontingSemanticBlobV2 || !json.Valid(value.ResponseJSON) ||
		value.CreatedAt <= 0 || value.UpdatedAt <= 0 || value.UpdatedAt < value.CreatedAt {
		return false
	}
	if value.Status == FrontingReceiptComplete {
		return frontingOpaqueV2(value.OperationID, 128) && value.OperationRevision > 0
	}
	return value.OperationID == "" && value.OperationRevision == 0 || frontingOpaqueV2(value.OperationID, 128) && value.OperationRevision > 0
}

// ValidFrontingCompletionV2 validates the bounded completion of an already
// claimed receipt. The Backend retains its exact pending-row CAS transaction.
func ValidFrontingCompletionV2(action, key, digest, operationID string, operationRevision int, response []byte, now int64) bool {
	return (action == "apply" || action == "rollback") && frontingOpaqueV2(key, 128) && frontingDigestV2(digest) &&
		frontingOpaqueV2(operationID, 128) && operationRevision > 0 && len(response) > 0 && len(response) <= MaxFrontingSemanticBlobV2 && json.Valid(response) && now > 0
}

func frontingStateDesiredV2(value string) bool {
	return value == "DISABLED" || value == "DISABLED_REPREVIEW_REQUIRED" || value == "L4_ONE_TO_ONE_FRONTING" || value == "SNI_PREREAD_FRONTING"
}

func frontingStateSelectedV2(value string) bool {
	return value == "" || value == "L4_ONE_TO_ONE_FRONTING" || value == "SNI_PREREAD_FRONTING"
}

func frontingStateActualV2(value string) bool {
	switch value {
	case "NOT_APPLIED", "PREPARED", "APPLYING", "HEALTH", "APPLIED", "DEGRADED", "ROLLING_BACK", "ROLLED_BACK", "ROLLBACK_FAILED", "RECONCILE_REQUIRED", "CANCELLED":
		return true
	default:
		return false
	}
}

func frontingRuntimeStateV2(value string) bool {
	switch value {
	case "UNKNOWN", "NGINX_NOT_INSTALLED", "NGINX_EXTERNAL_MANAGED", "NGINX_IDENTITY_UNKNOWN", "STREAM_UNAVAILABLE", "SSL_PREREAD_UNAVAILABLE", "PROXY_PROTOCOL_UNPROVEN", "VALIDATION_UNAVAILABLE", "RELOAD_UNAVAILABLE", "MANAGED_ENGINE_READY":
		return true
	default:
		return false
	}
}

func frontingInstallationClassV2(value string) bool {
	return value == "UNKNOWN" || value == "SOLOVEY_MANAGED" || value == "EXTERNAL_MANAGED" || value == "DEVELOPMENT_READ_ONLY"
}

func frontingOpaqueV2(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func frontingDigestV2(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}
