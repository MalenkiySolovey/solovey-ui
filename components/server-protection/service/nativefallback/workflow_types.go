package nativefallback

import (
	"encoding/json"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	NativeHealthFactsSchemaV1    = "solovey-ui/native-fallback-health/v1"
	NativeRecoveryBundleSchemaV1 = "solovey-ui/native-fallback-recovery/v1"
	providerCallTimeout          = 3 * time.Second
	workflowRecoveryTimeout      = 15 * time.Second
)

type PrepareWorkflowRequestV1 struct {
	Actor          string
	IdempotencyKey string
	Confirmation   string
	Plan           domain.NativeFallbackPlanV1
	PlanRequest    PlanRequestV1
}

type ApplyWorkflowRequestV1 struct {
	Actor                       string
	IdempotencyKey              string
	OperationID                 string
	OperationRevision           int
	PlanDigest                  string
	ProviderReservationRevision string
	ExpectedState               domain.NativeFallbackActualState
	Confirmed                   bool
}

type RollbackWorkflowRequestV1 struct {
	Actor                       string
	IdempotencyKey              string
	OperationID                 string
	OperationRevision           int
	PlanDigest                  string
	ProviderReservationRevision string
	Confirmed                   bool
}

type WorkflowResultV1 struct {
	Operation repository.NativeFallbackOperationModel
	State     domain.NativeFallbackStateV1
}

type NativeHealthFactsV1 struct {
	Schema                      string                                    `json:"schema"`
	OperationID                 string                                    `json:"operationId"`
	ResourceID                  string                                    `json:"resourceId"`
	RuntimeIdentityRevision     string                                    `json:"runtimeIdentityRevision"`
	InboundTag                  string                                    `json:"inboundTag"`
	InboundType                 string                                    `json:"inboundType"`
	EffectiveOptionsDigest      string                                    `json:"effectiveOptionsDigest"`
	ManagerGeneration           uint64                                    `json:"managerGeneration"`
	AfterConfigurationRevision  string                                    `json:"afterConfigurationRevision"`
	EffectiveRevision           string                                    `json:"effectiveRevision"`
	TargetReference             neutralfallback.FallbackTargetReferenceV2 `json:"targetReference"`
	ProviderReservationRevision string                                    `json:"providerReservationRevision"`
	ProviderHealthRevision      string                                    `json:"providerHealthRevision"`
	ProviderCapacityRevision    string                                    `json:"providerCapacityRevision"`
	ConnectFirstByteP95MS       *uint32                                   `json:"connectFirstByteP95Ms,omitempty"`
	TransportSecurity           neutralfallback.TransportSecurity         `json:"transportSecurity"`
	ApplicationProtocols        []neutralfallback.ApplicationProtocol     `json:"applicationProtocols"`
	RequiredServerNameDigest    string                                    `json:"requiredServerNameDigest,omitempty"`
	ManagementRevision          string                                    `json:"managementRevision"`
	ObservedAt                  time.Time                                 `json:"observedAt"`
	ExpiresAt                   time.Time                                 `json:"expiresAt"`
}

type NativeRecoveryBundleV1 struct {
	Schema                       string   `json:"schema"`
	OperationID                  string   `json:"operationId"`
	ResourceID                   string   `json:"resourceId"`
	ExpectedBeforeRevision       string   `json:"expectedBeforeRevision"`
	ExpectedAfterRevision        string   `json:"expectedAfterRevision"`
	CurrentConfigurationRevision string   `json:"currentConfigurationRevision,omitempty"`
	CheckpointStatus             string   `json:"checkpointStatus"`
	ProviderReservationStatus    string   `json:"providerReservationStatus"`
	FailedStage                  string   `json:"failedStage"`
	ReasonCodes                  []string `json:"reasonCodes"`
	PermittedNextAction          string   `json:"permittedNextAction"`
}

type WorkflowError struct {
	Code      string
	Ambiguous bool
}

func (err *WorkflowError) Error() string {
	if err == nil || err.Code == "" {
		return "native fallback workflow failed"
	}
	return err.Code
}

func decodeOperationPlan(operation repository.NativeFallbackOperationModel) (domain.NativeFallbackPlanV1, error) {
	var plan domain.NativeFallbackPlanV1
	if json.Unmarshal(operation.PlanJSON, &plan) != nil || plan.Validate() != nil || plan.PlanDigest != operation.PlanDigest {
		return domain.NativeFallbackPlanV1{}, &WorkflowError{Code: "operation_plan_invalid"}
	}
	return plan, nil
}
