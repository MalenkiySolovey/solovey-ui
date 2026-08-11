//go:build !minimal

package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionnativefallback "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/nativefallback"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"github.com/gin-gonic/gin"
)

const nativeFallbackBodyLimit = 64 << 10

type nativeFallbackPreviewRequest struct {
	ResourceID             string                                    `json:"resourceId"`
	ExpectedConfigRevision string                                    `json:"expectedConfigRevision"`
	TargetReference        neutralfallback.FallbackTargetReferenceV2 `json:"targetReference"`
}

type nativeFallbackPrepareRequest struct {
	PlanID                       string                                    `json:"planId"`
	PlanDigest                   string                                    `json:"planDigest"`
	ResourceID                   string                                    `json:"resourceId"`
	SourceRevision               string                                    `json:"sourceRevision"`
	ResourceRevision             string                                    `json:"resourceRevision"`
	ConfigurationRevision        string                                    `json:"configurationRevision"`
	EffectiveRevision            string                                    `json:"effectiveRevision"`
	RuntimeIdentityRevision      string                                    `json:"runtimeIdentityRevision"`
	CapabilityResolverRevision   string                                    `json:"capabilityResolverRevision"`
	CanonicalTargetRevision      string                                    `json:"canonicalTargetRevision"`
	ProviderRevision             string                                    `json:"providerRevision"`
	EndpointRevision             string                                    `json:"endpointRevision"`
	PublishRevision              string                                    `json:"publishRevision"`
	HealthRevision               string                                    `json:"healthRevision"`
	CapacityRevision             string                                    `json:"capacityRevision"`
	TargetReference              neutralfallback.FallbackTargetReferenceV2 `json:"targetReference"`
	IdempotencyKey               string                                    `json:"idempotencyKey"`
	ExperimentalRiskAcknowledged bool                                      `json:"experimentalRiskAcknowledged"`
}

type nativeFallbackApplyRequest struct {
	OperationID                 string `json:"operationId"`
	OperationRevision           int    `json:"operationRevision"`
	PlanDigest                  string `json:"planDigest"`
	ProviderReservationRevision string `json:"providerReservationRevision"`
	IdempotencyKey              string `json:"idempotencyKey"`
	Confirmation                string `json:"confirmation"`
}

type nativeFallbackRollbackRequest struct {
	OperationID                 string `json:"operationId"`
	OperationRevision           int    `json:"operationRevision"`
	PlanDigest                  string `json:"planDigest"`
	ProviderReservationRevision string `json:"providerReservationRevision"`
	IdempotencyKey              string `json:"idempotencyKey"`
	Confirmation                string `json:"confirmation"`
}

type nativeFallbackOperationView struct {
	OperationID                 string                           `json:"operationId"`
	ResourceID                  string                           `json:"resourceId"`
	Revision                    int                              `json:"revision"`
	State                       string                           `json:"state"`
	PlanDigest                  string                           `json:"planDigest"`
	ProviderReservationState    string                           `json:"providerReservationState,omitempty"`
	ProviderReservationRevision string                           `json:"providerReservationRevision,omitempty"`
	ActualState                 domain.NativeFallbackActualState `json:"actualState"`
	RecoveryRequired            bool                             `json:"recoveryRequired"`
	ReasonCodes                 []string                         `json:"reasonCodes,omitempty"`
	CreatedAt                   int64                            `json:"createdAt"`
	UpdatedAt                   int64                            `json:"updatedAt"`
}

type nativeFallbackStatusView struct {
	ResourceID            string                            `json:"resourceId"`
	Inbound               nativeInboundView                 `json:"inbound"`
	DesiredState          domain.NativeFallbackDesiredState `json:"desiredState"`
	SelectedVariant       domain.NativeFallbackVariant      `json:"selectedVariant"`
	ActualState           domain.NativeFallbackActualState  `json:"actualState"`
	Runtime               nativeRuntimeView                 `json:"runtime"`
	Capability            nativeCapabilityView              `json:"capability"`
	ConfigurationRevision string                            `json:"configurationRevision,omitempty"`
	EffectiveRevision     string                            `json:"effectiveRevision,omitempty"`
	Target                *nativeTargetSummary              `json:"target,omitempty"`
	ProviderReservation   *nativeReservationSummary         `json:"providerReservation,omitempty"`
	LatestOperation       *nativeFallbackOperationView      `json:"latestOperation,omitempty"`
	RecoveryStatus        string                            `json:"recoveryStatus"`
	ApplyGate             domain.NativeFallbackApplyGate    `json:"applyGate"`
	Blocks                []string                          `json:"blocks,omitempty"`
	Warnings              []string                          `json:"warnings,omitempty"`
	ReasonCodes           []string                          `json:"reasonCodes,omitempty"`
	UpdatedAt             int64                             `json:"updatedAt,omitempty"`
}

type nativeInboundView struct {
	DatabaseID uint   `json:"databaseId"`
	Tag        string `json:"tag"`
	Type       string `json:"type"`
}

type nativeRuntimeView struct {
	Status   string `json:"status"`
	Revision string `json:"revision,omitempty"`
}

type nativeCapabilityView struct {
	Status                        string `json:"status"`
	Revision                      string `json:"revision,omitempty"`
	Variant                       string `json:"variant"`
	NaturalInvalidTrafficFallback bool   `json:"naturalInvalidTrafficFallback"`
	ForcedSameSubjectDecoyRoute   bool   `json:"forcedSameSubjectDecoyRoute"`
}

type nativeTargetSummary struct {
	Identity             neutralfallback.TargetIdentity            `json:"identity"`
	Reference            neutralfallback.FallbackTargetReferenceV2 `json:"reference"`
	EndpointID           string                                    `json:"endpointId"`
	EndpointMode         string                                    `json:"endpointMode"`
	TransportSecurity    neutralfallback.TransportSecurity         `json:"transportSecurity"`
	ApplicationProtocols []neutralfallback.ApplicationProtocol     `json:"applicationProtocols"`
	AcceptedServerNames  int                                       `json:"acceptedServerNameCount"`
	Health               nativeHealthSummary                       `json:"health"`
	Capacity             nativeCapacitySummary                     `json:"capacity"`
	ProviderRevision     string                                    `json:"providerRevision"`
	Actionable           bool                                      `json:"actionable"`
	ReasonCodes          []string                                  `json:"reasonCodes,omitempty"`
}

type nativeHealthSummary struct {
	State      string   `json:"state"`
	Revision   string   `json:"revision,omitempty"`
	ObservedAt int64    `json:"observedAt,omitempty"`
	ExpiresAt  int64    `json:"expiresAt,omitempty"`
	Fresh      bool     `json:"fresh"`
	Reasons    []string `json:"reasonCodes,omitempty"`
}

type nativeCapacitySummary struct {
	State      string   `json:"state"`
	Revision   string   `json:"revision,omitempty"`
	Total      uint32   `json:"slotsTotal"`
	Used       uint32   `json:"slotsUsed"`
	ObservedAt int64    `json:"observedAt,omitempty"`
	ExpiresAt  int64    `json:"expiresAt,omitempty"`
	Fresh      bool     `json:"fresh"`
	Reasons    []string `json:"reasonCodes,omitempty"`
}

type nativeReservationSummary struct {
	State     string `json:"state"`
	Revision  string `json:"revision,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	Fresh     bool   `json:"fresh"`
}

func (h Handler) nativeFallbackStatus(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	if h.deps.NativeFallback == nil {
		writeNativeError(c, errors.New("native_workflow_unavailable"))
		return
	}
	ctx, now := c.Request.Context(), time.Now().UTC()
	inventory := protectionresources.Snapshot(ctx, false)
	resourceFilter := strings.TrimSpace(c.Query("resource_id"))
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(ctx)
	if err != nil {
		writeNativeError(c, err)
		return
	}
	applyGate := nativeApplyGate(settings.FeatureFlags["enable_apply_beta"], settings.AdvancedAcknowledgedAt)
	operations, err := h.deps.Repository.ListNativeFallbackOperations(ctx, nil)
	if err != nil {
		writeNativeError(c, err)
		return
	}
	latest := make(map[string]protectionrepository.NativeFallbackOperationModel)
	for _, operation := range operations {
		latest[operation.ResourceID] = operation
	}
	targets := neutralfallback.Default.SnapshotV2(ctx, now)
	reservations, _ := neutralfallback.Default.ListReservationsV2(ctx, neutralfallback.ListReservationsQueryV1{Limit: neutralfallback.MaxReservationListPageV2})
	items := make([]nativeFallbackStatusView, 0)
	for _, resource := range inventory.Resources {
		inboundID, ok := nativeInboundID(resource.ID)
		if !ok || resourceFilter != "" && resourceFilter != resource.ID {
			continue
		}
		identity, snapshot, inspectErr := h.deps.NativeFallback.Inspect(ctx, inboundID)
		state, stateErr := h.deps.Repository.NativeFallbackState(ctx, resource.ID)
		if stateErr != nil {
			writeNativeError(c, stateErr)
			return
		}
		view := nativeFallbackStatusView{
			ResourceID: resource.ID, Inbound: nativeInboundView{DatabaseID: inboundID, Tag: resource.InboundTag, Type: snapshot.Type},
			DesiredState: state.DesiredState, SelectedVariant: state.SelectedVariant, ActualState: state.ActualState,
			ConfigurationRevision: snapshot.ConfigurationRevision, EffectiveRevision: snapshot.Effective.Revision,
			Runtime: nativeRuntimeView{Status: string(identity.State), Revision: identity.IdentityRevision},
			Capability: nativeCapabilityView{Status: string(snapshot.Capability.Disposition), Revision: snapshot.CapabilityResolverRevision,
				Variant: string(snapshot.Capability.Variant), NaturalInvalidTrafficFallback: snapshot.Capability.NaturalInvalidTrafficFallback,
				ForcedSameSubjectDecoyRoute: snapshot.Capability.ForcedSameSubjectDecoyRoute},
			ApplyGate: applyGate, RecoveryStatus: nativeRecoveryStatus(state.ActualState),
			ReasonCodes: nativeReasonStrings(state.ReasonCodes),
		}
		if !state.UpdatedAt.IsZero() {
			view.UpdatedAt = state.UpdatedAt.Unix()
		}
		if inspectErr != nil {
			view.Runtime.Status = "unknown"
			view.Capability.Status = "unknown"
			view.Blocks = append(view.Blocks, "native_runtime_unknown")
		}
		if operation, exists := latest[resource.ID]; exists {
			operationView := projectNativeOperation(operation, state)
			view.LatestOperation = &operationView
			var reference neutralfallback.FallbackTargetReferenceV2
			if json.Unmarshal(operation.TargetReferenceJSON, &reference) == nil && reference.Validate() == nil {
				if target, found := exactTarget(targets.Targets, reference); found {
					summary := projectNativeTarget(target, now)
					view.Target = &summary
					if nativeStateNeedsAuthority(state.ActualState) {
						switch {
						case !summary.Health.Fresh:
							view.Blocks = append(view.Blocks, "target_health_stale")
						case !summary.Capacity.Fresh:
							view.Blocks = append(view.Blocks, "target_capacity_stale")
						case summary.Capacity.State == string(neutralfallback.CapacityExhausted):
							view.Blocks = append(view.Blocks, "target_capacity_exhausted")
						case !summary.Actionable:
							view.Blocks = append(view.Blocks, "target_unavailable")
						}
					}
				} else if nativeStateNeedsAuthority(state.ActualState) {
					view.Blocks = append(view.Blocks, "target_reference_stale")
				}
			}
			reservationMatched := false
			for _, reservation := range reservations.Reservations {
				if reservation.ReservationID == operation.ProviderReservationID {
					status := reservation.Status(now)
					view.ProviderReservation = &nativeReservationSummary{State: string(status.EffectiveState), Revision: reservation.ReservationRevision, ExpiresAt: reservation.FreshnessExpiresAt, Fresh: status.Fresh}
					reservationMatched = reservation.HolderID == operation.OperationID && reservation.ReservationRevision == operation.ProviderReservationRevision
					break
				}
			}
			if nativeStateNeedsAuthority(state.ActualState) && !reservationMatched {
				view.Blocks = append(view.Blocks, "provider_reservation_conflict")
			}
		}
		view.Blocks = boundedStrings(view.Blocks, 32)
		view.Warnings = boundedStrings(append(view.Warnings, snapshotReasonStrings(snapshot.ReasonCodes)...), 32)
		view.ReasonCodes = boundedStrings(append(append([]string{}, view.ReasonCodes...), append(view.Blocks, view.Warnings...)...), 32)
		items = append(items, view)
	}
	page := parsePage(c, 50, 200)
	paged, total := paginate(items, page)
	h.deps.JSONObj(c, gin.H{"items": paged, "page": page.Page, "limit": page.Limit, "total": total, "generatedAt": now.Unix()}, nil)
}

func (h Handler) nativeFallbackPreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input nativeFallbackPreviewRequest
	if !decodeStrictJSON(c, &input) {
		return
	}
	inboundID, ok := nativeInboundID(input.ResourceID)
	if !ok || !domain.ValidSHA256(input.ExpectedConfigRevision) || input.TargetReference.Validate() != nil || h.deps.NativeFallback == nil {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return
	}
	_, snapshot, err := h.deps.NativeFallback.Inspect(c.Request.Context(), inboundID)
	if err != nil || snapshot.ResourceID != input.ResourceID {
		writeNativeError(c, err)
		return
	}
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		writeNativeError(c, err)
		return
	}
	request := exactPlanRequest(snapshot, input.TargetReference, nativeApplyGate(settings.FeatureFlags["enable_apply_beta"], settings.AdvancedAcknowledgedAt))
	request.ExpectedConfigurationRevision = input.ExpectedConfigRevision
	h.audit(c, "server_protection_native_fallback_preview_requested", map[string]any{"resourceId": input.ResourceID})
	plan, err := h.deps.NativeFallback.Preview(c.Request.Context(), request)
	if err != nil {
		code, _ := nativeErrorCode(err)
		h.audit(c, "server_protection_native_fallback_preview_completed", map[string]any{"resourceId": input.ResourceID, "success": false, "code": code})
		writeNativeError(c, err)
		return
	}
	h.audit(c, "server_protection_native_fallback_preview_completed", map[string]any{"resourceId": input.ResourceID, "eligible": plan.Eligible, "actual": plan.ActualState})
	h.deps.JSONObj(c, plan, nil)
}

func (h Handler) nativeFallbackPrepare(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input nativeFallbackPrepareRequest
	if !decodeStrictJSON(c, &input) {
		return
	}
	if !input.ExperimentalRiskAcknowledged {
		writeNativeCode(c, http.StatusBadRequest, "experimental_ack_required")
		return
	}
	if !validNativeIdempotency(input.IdempotencyKey) || input.PlanID != input.PlanDigest || !domain.ValidSHA256(input.PlanDigest) || input.TargetReference.Validate() != nil {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return
	}
	inboundID, ok := nativeInboundID(input.ResourceID)
	if !ok || h.deps.NativeFallback == nil {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return
	}
	auditResult := map[string]any{"resourceId": input.ResourceID, "planId": input.PlanID, "success": false}
	h.audit(c, "server_protection_native_fallback_prepare_requested", map[string]any{"resourceId": input.ResourceID, "planId": input.PlanID})
	defer h.audit(c, "server_protection_native_fallback_prepare_completed", auditResult)
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		writeNativeError(c, err)
		return
	}
	if nativeApplyGate(settings.FeatureFlags["enable_apply_beta"], settings.AdvancedAcknowledgedAt) != domain.NativeApplyExperimental {
		writeNativeCode(c, http.StatusConflict, "apply_gate_disabled")
		return
	}
	planRequest := protectionnativefallback.PlanRequestV1{
		InboundDatabaseID: inboundID, ExpectedResourceID: input.ResourceID, ExpectedSourceRevision: input.SourceRevision,
		ExpectedResourceRevision: input.ResourceRevision, ExpectedConfigurationRevision: input.ConfigurationRevision,
		ExpectedEffectiveRevision: input.EffectiveRevision, TargetReference: input.TargetReference, ApplyGate: string(domain.NativeApplyExperimental),
	}
	plan, err := h.deps.NativeFallback.Preview(c.Request.Context(), planRequest)
	if err != nil {
		writeNativeError(c, err)
		return
	}
	if !plan.Eligible || !plan.ExpiresAt.After(time.Now().UTC()) || !prepareBindingsMatch(input, plan) {
		writeNativeCode(c, http.StatusConflict, "plan_digest_mismatch")
		return
	}
	result, err := h.deps.NativeFallback.Prepare(c.Request.Context(), protectionnativefallback.PrepareWorkflowRequestV1{
		Actor: h.actor(c), IdempotencyKey: input.IdempotencyKey, Confirmation: "PREPARE NATIVE FALLBACK " + plan.PlanDigest, Plan: plan, PlanRequest: planRequest,
	})
	if err != nil {
		code, _ := nativeErrorCode(err)
		auditResult["code"] = code
		writeNativeError(c, err)
		return
	}
	view := projectNativeOperation(result.Operation, result.State)
	auditResult["success"], auditResult["operationId"], auditResult["state"] = true, view.OperationID, view.State
	h.deps.JSONObj(c, view, nil)
}

func (h Handler) nativeFallbackApply(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input nativeFallbackApplyRequest
	if !decodeStrictJSON(c, &input) {
		return
	}
	if !validNativeMutationRequest(input.OperationID, input.OperationRevision, input.PlanDigest, input.ProviderReservationRevision, input.IdempotencyKey) {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return
	}
	if !exactConfirmation(input.Confirmation, "APPLY NATIVE FALLBACK "+input.OperationID) {
		writeNativeCode(c, http.StatusBadRequest, "confirmation_mismatch")
		return
	}
	auditResult := map[string]any{"operationId": input.OperationID, "operationRevision": input.OperationRevision, "success": false}
	h.audit(c, "server_protection_native_fallback_apply_requested", map[string]any{"operationId": input.OperationID, "operationRevision": input.OperationRevision})
	defer h.audit(c, "server_protection_native_fallback_apply_completed", auditResult)
	if !h.nativeApplyConfigured(c) {
		return
	}
	result, err := h.deps.NativeFallback.Apply(c.Request.Context(), protectionnativefallback.ApplyWorkflowRequestV1{
		Actor: h.actor(c), IdempotencyKey: input.IdempotencyKey, OperationID: input.OperationID, OperationRevision: input.OperationRevision, PlanDigest: input.PlanDigest,
		ProviderReservationRevision: input.ProviderReservationRevision, ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err != nil {
		code, _ := nativeErrorCode(err)
		auditResult["code"] = code
		writeNativeError(c, err)
		return
	}
	view := projectNativeOperation(result.Operation, result.State)
	auditResult["success"], auditResult["state"], auditResult["actual"] = true, view.State, view.ActualState
	h.deps.JSONObj(c, view, nil)
}

func (h Handler) nativeFallbackRollback(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input nativeFallbackRollbackRequest
	if !decodeStrictJSON(c, &input) {
		return
	}
	if !validNativeMutationRequest(input.OperationID, input.OperationRevision, input.PlanDigest, input.ProviderReservationRevision, input.IdempotencyKey) {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return
	}
	if !exactConfirmation(input.Confirmation, "ROLLBACK NATIVE FALLBACK "+input.OperationID) {
		writeNativeCode(c, http.StatusBadRequest, "confirmation_mismatch")
		return
	}
	auditResult := map[string]any{"operationId": input.OperationID, "operationRevision": input.OperationRevision, "success": false}
	h.audit(c, "server_protection_native_fallback_rollback_requested", map[string]any{"operationId": input.OperationID, "operationRevision": input.OperationRevision})
	defer h.audit(c, "server_protection_native_fallback_rollback_completed", auditResult)
	if !h.nativeApplyConfigured(c) {
		return
	}
	result, err := h.deps.NativeFallback.Rollback(c.Request.Context(), protectionnativefallback.RollbackWorkflowRequestV1{
		Actor: h.actor(c), IdempotencyKey: input.IdempotencyKey, OperationID: input.OperationID, OperationRevision: input.OperationRevision, PlanDigest: input.PlanDigest,
		ProviderReservationRevision: input.ProviderReservationRevision, Confirmed: true,
	})
	if err != nil {
		code, _ := nativeErrorCode(err)
		auditResult["code"] = code
		writeNativeError(c, err)
		return
	}
	view := projectNativeOperation(result.Operation, result.State)
	auditResult["success"], auditResult["state"], auditResult["actual"] = true, view.State, view.ActualState
	h.deps.JSONObj(c, view, nil)
}

func (h Handler) nativeApplyConfigured(c *gin.Context) bool {
	if h.deps.NativeFallback == nil {
		writeNativeCode(c, http.StatusServiceUnavailable, "target_unavailable")
		return false
	}
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		writeNativeError(c, err)
		return false
	}
	if nativeApplyGate(settings.FeatureFlags["enable_apply_beta"], settings.AdvancedAcknowledgedAt) != domain.NativeApplyExperimental {
		writeNativeCode(c, http.StatusConflict, "apply_gate_disabled")
		return false
	}
	return true
}

func decodeStrictJSON(c *gin.Context, value any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeNativeCode(c, http.StatusUnsupportedMediaType, "malformed_input")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, nativeFallbackBodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeNativeCode(c, http.StatusBadRequest, "malformed_input")
		return false
	}
	return true
}

func writeNativeError(c *gin.Context, err error) {
	code, status := nativeErrorCode(err)
	writeNativeCode(c, status, code)
}

func writeNativeCode(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": code}})
}

func nativeErrorCode(err error) (string, int) {
	if err == nil {
		return "internal_failure", http.StatusInternalServerError
	}
	var plannerErr *protectionnativefallback.PlannerError
	var workflowErr *protectionnativefallback.WorkflowError
	code := ""
	ambiguous := false
	if errors.As(err, &plannerErr) {
		code = plannerErr.Code
	}
	if errors.As(err, &workflowErr) {
		code, ambiguous = workflowErr.Code, workflowErr.Ambiguous
	}
	if ambiguous {
		return "ambiguous_result", http.StatusConflict
	}
	lower := strings.ToLower(code)
	switch {
	case code == "target_reference_stale" || code == "target_revision_drift":
		return "target_reference_stale", http.StatusConflict
	case code == "prepared_plan_expired":
		return "plan_expired", http.StatusConflict
	case code == "prepare_plan_stale_or_blocked":
		return "plan_digest_mismatch", http.StatusConflict
	case code == "prepare_idempotency_conflict":
		return "operation_conflict", http.StatusConflict
	case code == "apply_operation_stale" || code == "rollback_operation_stale":
		return "operation_revision_stale", http.StatusConflict
	case code == "provider_reservation_stale" || code == "reservation_mirror_mismatch":
		return "provider_reservation_conflict", http.StatusConflict
	case code == "health_observation_expired" || code == "provider_health_fence_invalid":
		return "target_health_stale", http.StatusConflict
	case code == "management_health_failed":
		return "management_target_forbidden", http.StatusConflict
	case code == "core_revision_drift" || code == "management_revision_drift" || strings.Contains(lower, "concurrent_core_drift"):
		return "reconcile_required", http.StatusConflict
	case code == "apply_gate_invalid":
		return "apply_gate_disabled", http.StatusConflict
	case code == "planner_input_invalid" || code == "plan_contract_invalid":
		return "malformed_input", http.StatusBadRequest
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		return "operation_not_found", http.StatusNotFound
	case errors.Is(err, protectionrepository.ErrRevisionConflict), strings.Contains(lower, "stale"):
		return "operation_revision_stale", http.StatusConflict
	case errors.Is(err, protectionrepository.ErrOperationConflict), errors.Is(err, protectionoperations.ErrConflict), strings.Contains(lower, "conflict"), strings.Contains(lower, "fence"):
		return "operation_conflict", http.StatusConflict
	case strings.Contains(lower, "runtime") && strings.Contains(lower, "unknown"):
		return "native_runtime_unknown", http.StatusUnprocessableEntity
	case strings.Contains(lower, "runtime"):
		return "native_runtime_unsupported", http.StatusUnprocessableEntity
	case strings.Contains(lower, "configuration") || strings.Contains(lower, "capability"):
		return "native_configuration_unsupported", http.StatusUnprocessableEntity
	case strings.Contains(lower, "target") || strings.Contains(lower, "provider"):
		return "target_unavailable", http.StatusServiceUnavailable
	case strings.Contains(lower, "rollback"):
		return "rollback_failed", http.StatusConflict
	case strings.Contains(lower, "health"):
		return "health_failed", http.StatusConflict
	case strings.Contains(lower, "cancel"):
		return "ambiguous_result", http.StatusConflict
	case strings.Contains(lower, "request") || strings.Contains(lower, "input") || strings.Contains(lower, "plan_contract"):
		return "malformed_input", http.StatusBadRequest
	default:
		return "internal_failure", http.StatusInternalServerError
	}
}

func exactPlanRequest(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, reference neutralfallback.FallbackTargetReferenceV2, gate domain.NativeFallbackApplyGate) protectionnativefallback.PlanRequestV1 {
	return protectionnativefallback.PlanRequestV1{
		InboundDatabaseID: snapshot.InboundDatabaseID, ExpectedResourceID: snapshot.ResourceID,
		ExpectedSourceRevision: protectionnativefallback.SourceRevision(snapshot), ExpectedResourceRevision: protectionnativefallback.ResourceRevision(snapshot),
		ExpectedConfigurationRevision: snapshot.ConfigurationRevision, ExpectedEffectiveRevision: snapshot.Effective.Revision,
		TargetReference: reference, ApplyGate: string(gate),
	}
}

func prepareBindingsMatch(input nativeFallbackPrepareRequest, plan domain.NativeFallbackPlanV1) bool {
	return input.PlanID == plan.PlanID && input.PlanDigest == plan.PlanDigest && input.ResourceID == plan.Resource.ResourceID &&
		input.SourceRevision == plan.Resource.SourceRevision && input.ResourceRevision == plan.Resource.ResourceRevision &&
		input.ConfigurationRevision == plan.Resource.ConfigurationRevision && input.EffectiveRevision == plan.Resource.EffectiveRevision &&
		input.RuntimeIdentityRevision == plan.Runtime.IdentityRevision && input.CapabilityResolverRevision == plan.Runtime.CapabilityResolverRevision &&
		input.CanonicalTargetRevision == plan.Target.CanonicalTargetRevision && input.ProviderRevision == plan.Target.ProviderRevision &&
		input.EndpointRevision == plan.Target.EndpointRevision && input.PublishRevision == plan.Target.PublishRevision &&
		input.HealthRevision == plan.Target.HealthRevision && input.CapacityRevision == plan.Target.CapacityRevision &&
		input.TargetReference == plan.Target.Reference && plan.ActualState == domain.NativeActualNotApplied
}

func projectNativeOperation(operation protectionrepository.NativeFallbackOperationModel, state domain.NativeFallbackStateV1) nativeFallbackOperationView {
	reasons := []string{}
	_ = json.Unmarshal(operation.ReasonCodesJSON, &reasons)
	return nativeFallbackOperationView{
		OperationID: operation.OperationID, ResourceID: operation.ResourceID, Revision: operation.Revision, State: operation.WorkflowState,
		PlanDigest: operation.PlanDigest, ProviderReservationRevision: operation.ProviderReservationRevision, ActualState: state.ActualState,
		RecoveryRequired: nativeRecoveryStatus(state.ActualState) == "required", ReasonCodes: boundedStrings(reasons, 32),
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt,
	}
}

func projectNativeTarget(target neutralfallback.FallbackTargetV2, now time.Time) nativeTargetSummary {
	reference, _ := neutralfallback.ReferenceV2FromTarget(target)
	healthState := neutralfallback.EffectiveReadinessV2(target.Health, now)
	capacityState := neutralfallback.EffectiveCapacityStateV2(target.Capacity, now)
	mode := "UNKNOWN"
	if target.Endpoint.TransportSecurity == neutralfallback.TransportSecurityTLS {
		mode = "TLS_HANDSHAKE_TARGET"
	} else if target.Endpoint.TransportSecurity == neutralfallback.TransportSecurityPlaintext {
		mode = "PLAINTEXT_POST_TLS_TARGET"
	}
	actionable := healthState == neutralfallback.ReadinessReady && capacityState == neutralfallback.CapacityReady &&
		target.Endpoint.Local && target.Endpoint.CanReachManagement == "no" && len(target.Health.ReasonCodes) == 0 && len(target.Capacity.ReasonCodes) == 0
	reasons := boundedStrings(append(append([]string{}, target.Health.ReasonCodes...), target.Capacity.ReasonCodes...), 32)
	return nativeTargetSummary{
		Identity: target.Identity, Reference: reference, EndpointID: target.Endpoint.EndpointID, EndpointMode: mode,
		TransportSecurity: target.Endpoint.TransportSecurity, ApplicationProtocols: append([]neutralfallback.ApplicationProtocol(nil), target.Endpoint.ApplicationProtocols...),
		AcceptedServerNames: len(target.Endpoint.AcceptedServerNames), ProviderRevision: target.ProviderRevision, Actionable: actionable, ReasonCodes: reasons,
		Health:   nativeHealthSummary{State: string(healthState), Revision: target.Health.Revision, ObservedAt: target.Health.ObservedAt, ExpiresAt: target.Health.ExpiresAt, Fresh: healthState != neutralfallback.ReadinessStale, Reasons: boundedStrings(target.Health.ReasonCodes, 32)},
		Capacity: nativeCapacitySummary{State: string(capacityState), Revision: target.Capacity.Revision, Total: target.Capacity.ReservationSlotsTotal, Used: target.Capacity.ReservationSlotsUsed, ObservedAt: target.Capacity.ObservedAt, ExpiresAt: target.Capacity.ExpiresAt, Fresh: capacityState != neutralfallback.CapacityStale, Reasons: boundedStrings(target.Capacity.ReasonCodes, 32)},
	}
}

func exactTarget(targets []neutralfallback.FallbackTargetV2, reference neutralfallback.FallbackTargetReferenceV2) (neutralfallback.FallbackTargetV2, bool) {
	for _, target := range targets {
		exact, err := neutralfallback.ReferenceV2FromTarget(target)
		if err == nil && exact == reference {
			return target, true
		}
	}
	return neutralfallback.FallbackTargetV2{}, false
}

func nativeInboundID(resourceID string) (uint, bool) {
	const prefix = "core:inbound:"
	if !strings.HasPrefix(resourceID, prefix) {
		return 0, false
	}
	value := strings.TrimPrefix(resourceID, prefix)
	if value == "" || strings.TrimLeft(value, "0123456789") != "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint(parsed), err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}

func nativeApplyGate(enabled bool, acknowledgedAt int64) domain.NativeFallbackApplyGate {
	if enabled && acknowledgedAt > 0 {
		return domain.NativeApplyExperimental
	}
	return domain.NativeApplyDisabledByDefault
}

func validNativeIdempotency(value string) bool {
	return len(value) >= 16 && domain.ValidContractID(value, 128)
}

func validNativeMutationRequest(operationID string, revision int, planDigest, reservationRevision, idempotencyKey string) bool {
	return domain.ValidContractID(operationID, 128) && revision > 0 && domain.ValidSHA256(planDigest) &&
		domain.ValidContractID(reservationRevision, 128) && validNativeIdempotency(idempotencyKey)
}

func exactConfirmation(actual, expected string) bool {
	return len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func nativeRecoveryStatus(state domain.NativeFallbackActualState) string {
	switch state {
	case domain.NativeActualRollbackFailed, domain.NativeActualReconcileRequired, domain.NativeActualDegraded:
		return "required"
	case domain.NativeActualApplying, domain.NativeActualHealth, domain.NativeActualRollingBack:
		return "in_progress"
	default:
		return "not_required"
	}
}

func nativeStateNeedsAuthority(state domain.NativeFallbackActualState) bool {
	switch state {
	case domain.NativeActualPrepared, domain.NativeActualApplying, domain.NativeActualHealth, domain.NativeActualApplied,
		domain.NativeActualDegraded, domain.NativeActualRollingBack, domain.NativeActualRollbackFailed, domain.NativeActualReconcileRequired:
		return true
	default:
		return false
	}
}

func nativeReasonStrings(values []domain.NativeFallbackReasonCode) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func snapshotReasonStrings(values []coreinboundcontrol.ReasonCode) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func boundedStrings(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !domain.ValidContractID(value, 128) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}
