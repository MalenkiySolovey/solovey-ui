package localproxy

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	PlanSchemaV1   = "solovey-ui/local-proxy-guard-plan/v1"
	StatusSchemaV1 = "solovey-ui/local-proxy-guard-status/v1"
	StateSchemaV1  = "solovey-ui/local-proxy-guard-state/v1"
	MaxPlanAgeV1   = 5 * time.Minute
)

type ActualState string

const (
	StateNotApplied          ActualState = "NOT_APPLIED"
	StatePrepared            ActualState = "PREPARED"
	StateApplying            ActualState = "APPLYING"
	StateHealth              ActualState = "HEALTH"
	StateAppliedExperimental ActualState = "APPLIED_EXPERIMENTAL"
	StateDegraded            ActualState = "DEGRADED"
	StateBlocked             ActualState = "BLOCKED"
	StateRollingBack         ActualState = "ROLLING_BACK"
	StateRecoveryRequired    ActualState = "RECOVERY_REQUIRED"
	StateExternalManaged     ActualState = "EXTERNAL_MANAGED"
	StateUnsupported         ActualState = "UNSUPPORTED"
)

type ApplyGate string

const (
	ApplyGateExperimentalAck ApplyGate = "EXPERIMENTAL_ACK_REQUIRED"
	ApplyGateBlocked         ApplyGate = "BLOCKED"
)

type PlanReferenceV1 struct {
	ResourceID   string `json:"resourceId"`
	EndpointID   string `json:"endpointId"`
	FactRevision string `json:"factRevision"`
}

type PlanV1 struct {
	Schema         string                               `json:"schema"`
	PlanID         string                               `json:"planId"`
	PlanDigest     string                               `json:"planDigest"`
	CreatedAt      int64                                `json:"createdAt"`
	ExpiresAt      int64                                `json:"expiresAt"`
	ResourceID     string                               `json:"resourceId"`
	EndpointID     string                               `json:"endpointId"`
	FactRevision   string                               `json:"factRevision"`
	Fact           hostresources.LocalProxyFactV1       `json:"fact"`
	ExactReference *hostresources.LocalProxyReferenceV1 `json:"exactReference,omitempty"`
	ActualState    ActualState                          `json:"actualState"`
	ApplyGate      ApplyGate                            `json:"applyGate"`
	BlockCodes     []string                             `json:"blockCodes,omitempty"`
	WarningCodes   []string                             `json:"warningCodes,omitempty"`
}

type StatusV1 struct {
	Schema              string                           `json:"schema"`
	GeneratedAt         int64                            `json:"generatedAt"`
	Facts               []hostresources.LocalProxyFactV1 `json:"facts"`
	Plans               []PlanV1                         `json:"plans"`
	States              []StateViewV1                    `json:"states"`
	Experimental        bool                             `json:"experimental"`
	DefaultApplyEnabled bool                             `json:"defaultApplyEnabled"`
	ReasonCodes         []string                         `json:"reasonCodes,omitempty"`
}

type StateViewV1 struct {
	ResourceID              string                                         `json:"resourceId"`
	EndpointID              string                                         `json:"endpointId"`
	ActualState             ActualState                                    `json:"actualState"`
	ApplyGate               ApplyGate                                      `json:"applyGate"`
	PlanID                  string                                         `json:"planId"`
	PlanDigest              string                                         `json:"planDigest"`
	FactRevision            string                                         `json:"factRevision"`
	Lease                   LeaseViewV1                                    `json:"lease"`
	LatestOperationID       string                                         `json:"latestOperationId"`
	LatestOperationRevision int                                            `json:"latestOperationRevision"`
	MarkerRevision          string                                         `json:"markerRevision,omitempty"`
	Health                  []componenthealth.LocalProxyProbeObservationV1 `json:"health"`
	HealthRevision          string                                         `json:"healthRevision,omitempty"`
	HealthExpiresUnixNano   int64                                          `json:"healthExpiresUnixNano,omitempty"`
	ProviderGuarded         bool                                           `json:"providerGuarded"`
	RecoveryRequired        bool                                           `json:"recoveryRequired"`
	UpdatedAt               int64                                          `json:"updatedAt"`
}

type PrepareRequestV1 struct {
	ResourceID     string `json:"resourceId"`
	EndpointID     string `json:"endpointId"`
	FactRevision   string `json:"factRevision"`
	PlanID         string `json:"planId"`
	PlanDigest     string `json:"planDigest"`
	IdempotencyKey string `json:"idempotencyKey"`
	Acknowledged   bool   `json:"acknowledged"`
	Confirmation   string `json:"confirmation"`
}

type ApplyRequestV1 struct {
	OperationID       string `json:"operationId"`
	OperationRevision int    `json:"operationRevision"`
	PlanID            string `json:"planId"`
	PlanDigest        string `json:"planDigest"`
	FactRevision      string `json:"factRevision"`
	IdempotencyKey    string `json:"idempotencyKey"`
	Acknowledged      bool   `json:"acknowledged"`
	Confirmation      string `json:"confirmation"`
}

type DisableRequestV1 struct {
	OperationID       string `json:"operationId"`
	OperationRevision int    `json:"operationRevision"`
	IdempotencyKey    string `json:"idempotencyKey"`
	Confirmation      string `json:"confirmation"`
}

type LeaseViewV1 struct {
	LeaseID   string                           `json:"leaseId"`
	Revision  string                           `json:"revision"`
	State     hostresources.EndpointLeaseState `json:"state"`
	RenewedAt int64                            `json:"renewedAt"`
	ExpiresAt int64                            `json:"expiresAt"`
}

type ResultV1 struct {
	OperationID       string                                         `json:"operationId"`
	OperationRevision int                                            `json:"operationRevision"`
	OperationState    string                                         `json:"operationState"`
	PlanID            string                                         `json:"planId"`
	PlanDigest        string                                         `json:"planDigest"`
	ActualState       ActualState                                    `json:"actualState"`
	Lease             LeaseViewV1                                    `json:"lease"`
	Health            []componenthealth.LocalProxyProbeObservationV1 `json:"health,omitempty"`
	Replayed          bool                                           `json:"replayed"`
	RecoveryRequired  bool                                           `json:"recoveryRequired"`
	WarningCodes      []string                                       `json:"warningCodes,omitempty"`
}

type RecoveryStatusV1 struct {
	OperationID      string      `json:"operationId"`
	ActualState      ActualState `json:"actualState"`
	ProviderGuarded  bool        `json:"providerGuarded"`
	RecoveryRequired bool        `json:"recoveryRequired"`
	SafeNextAction   string      `json:"safeNextAction"`
	ReasonCodes      []string    `json:"reasonCodes,omitempty"`
}

func (p PlanV1) Validate(now time.Time) error {
	if p.Schema != PlanSchemaV1 || !safeID(p.PlanID, 96) || !digest(p.PlanDigest) ||
		!safeID(p.ResourceID, 256) || !safeID(p.EndpointID, 128) || !digest(p.FactRevision) ||
		p.Fact.ResourceID != p.ResourceID || p.Fact.EndpointID != p.EndpointID || p.Fact.FactRevision != p.FactRevision ||
		p.Fact.Validate(time.Time{}) != nil || p.CreatedAt <= 0 || p.ExpiresAt <= p.CreatedAt ||
		p.ExpiresAt-p.CreatedAt > int64(MaxPlanAgeV1/time.Second) || !now.IsZero() && p.ExpiresAt <= now.UTC().Unix() ||
		!validActualState(p.ActualState) || (p.ApplyGate != ApplyGateExperimentalAck && p.ApplyGate != ApplyGateBlocked) ||
		!slices.Equal(codes(p.BlockCodes), p.BlockCodes) || !slices.Equal(codes(p.WarningCodes), p.WarningCodes) ||
		p.PlanDigest != planDigest(p) || p.PlanID != "local-proxy-plan:"+p.PlanDigest[:32] {
		return errors.New("local_proxy_plan_v1_invalid")
	}
	if p.ApplyGate == ApplyGateExperimentalAck {
		if len(p.BlockCodes) != 0 || p.ExactReference == nil || p.ExactReference.Validate() != nil ||
			p.ExactReference.CanonicalReferenceRevision == "" {
			return errors.New("local_proxy_plan_v1_actionable_binding_invalid")
		}
	} else if len(p.BlockCodes) == 0 || p.ExactReference != nil {
		return errors.New("local_proxy_plan_v1_block_binding_invalid")
	}
	return nil
}

func finalizePlan(value PlanV1) PlanV1 {
	value.Schema = PlanSchemaV1
	value.BlockCodes, value.WarningCodes = codes(value.BlockCodes), codes(value.WarningCodes)
	value.PlanID, value.PlanDigest = "", ""
	value.PlanDigest = planDigest(value)
	value.PlanID = "local-proxy-plan:" + value.PlanDigest[:32]
	return value
}

func planDigest(value PlanV1) string {
	value.PlanID, value.PlanDigest = "", ""
	value.CreatedAt, value.ExpiresAt = 0, 0
	value.ActualState = StateNotApplied
	return hostresources.Revision(value)
}

func leaseView(value hostresources.LocalProxyGuardLeaseV1) LeaseViewV1 {
	return LeaseViewV1{LeaseID: value.LeaseID, Revision: value.LeaseRevision, State: value.State, RenewedAt: value.RenewedAt, ExpiresAt: value.ExpiresAt}
}

func validActualState(value ActualState) bool {
	switch value {
	case StateNotApplied, StatePrepared, StateApplying, StateHealth, StateAppliedExperimental, StateDegraded,
		StateBlocked, StateRollingBack, StateRecoveryRequired, StateExternalManaged, StateUnsupported:
		return true
	default:
		return false
	}
}

func safeID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._:@+-", character) {
			continue
		}
		return false
	}
	return true
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func codes(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" && len(value) <= 96 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == 32 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func allHealthRevision(values []componenthealth.LocalProxyProbeObservationV1) string {
	if len(values) == 0 {
		return ""
	}
	copies := append([]componenthealth.LocalProxyProbeObservationV1(nil), values...)
	sort.Slice(copies, func(i, j int) bool { return copies[i].Protocol < copies[j].Protocol })
	return hostresources.Revision(copies)
}
