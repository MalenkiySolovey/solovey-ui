//go:build !minimal

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	"github.com/gin-gonic/gin"
)

type frontingSemanticService interface {
	Status(context.Context) (protectionfronting.FrontingStatusPageV2, error)
	Preview(context.Context, protectionfronting.FrontingPreviewRequestV2) (protectionfronting.FrontingStrategyPlanV2, error)
	Prepare(context.Context, protectionfronting.FrontingPrepareRequestV2, string) (protectionfronting.FrontingOperationViewV2, error)
	Apply(context.Context, protectionfronting.FrontingApplyRequestV2) (protectionfronting.FrontingOperationViewV2, error)
	Rollback(context.Context, protectionfronting.FrontingRollbackRequestV2) (protectionfronting.FrontingOperationViewV2, error)
	Operation(context.Context, string) (protectionfronting.FrontingOperationViewV2, error)
	Recovery(context.Context, string) (protectionfronting.FrontingRecoveryStatusV2, error)
}

func (h Handler) frontingStatus(c *gin.Context) {
	if !h.frontingReadAllowed(c) {
		return
	}
	if h.deps.FrontingV2 == nil {
		writeFrontingCode(c, http.StatusServiceUnavailable, "validation_unavailable")
		return
	}
	status, err := h.deps.FrontingV2.Status(c.Request.Context())
	if err != nil {
		writeFrontingError(c, err)
		return
	}
	h.deps.JSONObj(c, status, nil)
}

func (h Handler) frontingPreview(c *gin.Context) {
	if !h.frontingWriteAllowed(c) {
		return
	}
	var request protectionfronting.FrontingPreviewRequestV2
	if !decodeStrictJSON(c, &request) {
		return
	}
	if h.deps.FrontingV2 == nil {
		writeFrontingCode(c, http.StatusServiceUnavailable, "validation_unavailable")
		return
	}
	h.audit(c, "server_protection_fronting_preview_requested", map[string]any{"resourceId": request.ResourceID, "strategy": request.RequestedStrategy})
	plan, err := h.deps.FrontingV2.Preview(c.Request.Context(), request)
	if err != nil {
		code, _ := frontingErrorCode(err)
		h.audit(c, "server_protection_fronting_preview_completed", map[string]any{"resourceId": request.ResourceID, "success": false, "code": code})
		writeFrontingError(c, err)
		return
	}
	h.audit(c, "server_protection_fronting_preview_completed", map[string]any{"resourceId": request.ResourceID, "success": true, "planId": plan.PlanID, "eligible": len(plan.Safety.Blocks) == 0})
	h.deps.JSONObj(c, plan, nil)
}

func (h Handler) frontingPrepare(c *gin.Context) {
	if !h.frontingApplyAllowed(c) {
		return
	}
	var request protectionfronting.FrontingPrepareRequestV2
	if !decodeStrictJSON(c, &request) {
		return
	}
	if !h.frontingApplyConfigured(c) {
		return
	}
	audit := map[string]any{"resourceId": request.ResourceID, "planId": request.PlanID, "success": false}
	h.audit(c, "server_protection_fronting_prepare_requested", map[string]any{"resourceId": request.ResourceID, "planId": request.PlanID})
	defer h.audit(c, "server_protection_fronting_prepare_completed", audit)
	result, err := h.deps.FrontingV2.Prepare(c.Request.Context(), request, h.actor(c))
	if err != nil {
		code, _ := frontingErrorCode(err)
		audit["code"] = code
		writeFrontingError(c, err)
		return
	}
	audit["success"], audit["operationId"], audit["state"] = true, result.OperationID, result.WorkflowState
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) frontingApply(c *gin.Context) {
	if !h.frontingApplyAllowed(c) {
		return
	}
	var request protectionfronting.FrontingApplyRequestV2
	if !decodeStrictJSON(c, &request) {
		return
	}
	if !h.frontingApplyConfigured(c) {
		return
	}
	h.writeFrontingMutation(c, "apply", request.OperationID, request.OperationRevision, func() (protectionfronting.FrontingOperationViewV2, error) {
		return h.deps.FrontingV2.Apply(c.Request.Context(), request)
	})
}

func (h Handler) frontingRollback(c *gin.Context) {
	if !h.frontingApplyAllowed(c) {
		return
	}
	var request protectionfronting.FrontingRollbackRequestV2
	if !decodeStrictJSON(c, &request) {
		return
	}
	if !h.frontingApplyConfigured(c) {
		return
	}
	h.writeFrontingMutation(c, "rollback", request.OperationID, request.OperationRevision, func() (protectionfronting.FrontingOperationViewV2, error) {
		return h.deps.FrontingV2.Rollback(c.Request.Context(), request)
	})
}

func (h Handler) writeFrontingMutation(c *gin.Context, action, operationID string, revision int, invoke func() (protectionfronting.FrontingOperationViewV2, error)) {
	audit := map[string]any{"operationId": operationID, "operationRevision": revision, "success": false}
	h.audit(c, "server_protection_fronting_"+action+"_requested", map[string]any{"operationId": operationID, "operationRevision": revision})
	defer h.audit(c, "server_protection_fronting_"+action+"_completed", audit)
	result, err := invoke()
	if err != nil {
		code, _ := frontingErrorCode(err)
		audit["code"] = code
		writeFrontingError(c, err)
		return
	}
	audit["success"], audit["state"], audit["actual"] = true, result.WorkflowState, result.ActualState
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) frontingOperation(c *gin.Context) {
	if !h.frontingReadAllowed(c) {
		return
	}
	if h.deps.FrontingV2 == nil {
		writeFrontingCode(c, http.StatusServiceUnavailable, "validation_unavailable")
		return
	}
	result, err := h.deps.FrontingV2.Operation(c.Request.Context(), c.Param("operationId"))
	if err != nil {
		writeFrontingError(c, err)
		return
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) frontingRecovery(c *gin.Context) {
	if !h.frontingReadAllowed(c) {
		return
	}
	if h.deps.FrontingV2 == nil {
		writeFrontingCode(c, http.StatusServiceUnavailable, "validation_unavailable")
		return
	}
	result, err := h.deps.FrontingV2.Recovery(c.Request.Context(), c.Param("operationId"))
	if err != nil {
		writeFrontingError(c, err)
		return
	}
	h.deps.JSONObj(c, result, nil)
}

// frontingRetiredWrite is the one-release compatibility tombstone. It does
// not parse, translate, preview, acquire, persist, or invoke the old workflow.
func (h Handler) frontingRetiredWrite(c *gin.Context) {
	if !h.frontingApplyAllowed(c) {
		return
	}
	h.audit(c, "server_protection_fronting_legacy_write_rejected", map[string]any{"route": "sync", "code": "legacy_fronting_write_retired"})
	writeFrontingCode(c, http.StatusGone, "legacy_fronting_write_retired")
}

func (h Handler) frontingReadAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", readScope, writeScope, applyScope)
}

func (h Handler) frontingWriteAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", writeScope)
}

func (h Handler) frontingApplyAllowed(c *gin.Context) bool {
	return h.deps.RequireScope(c, "serverProtection", applyScope)
}

func (h Handler) frontingApplyConfigured(c *gin.Context) bool {
	if h.deps.FrontingV2 == nil || h.deps.Repository == nil {
		writeFrontingCode(c, http.StatusServiceUnavailable, "validation_unavailable")
		return false
	}
	settings, _, _, err := h.deps.Repository.LoadSettingsRevision(c.Request.Context())
	if err != nil {
		writeFrontingCode(c, http.StatusInternalServerError, "internal_failure")
		return false
	}
	if !settings.Enabled || !settings.FeatureFlags["enable_fronting_beta"] || settings.AdvancedAcknowledgedAt == 0 {
		writeFrontingCode(c, http.StatusConflict, "apply_gate_disabled")
		return false
	}
	return true
}

func writeFrontingError(c *gin.Context, err error) {
	code, status := frontingErrorCode(err)
	writeFrontingCode(c, status, code)
}

func writeFrontingCode(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": code}})
}

func frontingErrorCode(err error) (string, int) {
	var semantic *protectionfronting.SemanticErrorV2
	if errors.As(err, &semantic) {
		if semantic.Ambiguous {
			return "ambiguous_result", http.StatusConflict
		}
		code := semantic.Code
		switch code {
		case "operation_not_found":
			return code, http.StatusNotFound
		case "confirmation_mismatch", "selector_invalid", "default_policy_invalid", "experimental_ack_required":
			return code, http.StatusBadRequest
		case "nginx_not_installed", "nginx_external_managed", "nginx_identity_unknown", "stream_unavailable", "ssl_preread_unavailable", "validation_unavailable", "reload_unavailable":
			return code, http.StatusServiceUnavailable
		case "alpn_routing_unsupported":
			return code, http.StatusUnprocessableEntity
		case "runtime_identity_stale", "capability_stale", "socket_claim_stale", "topology_mutation_blocked", "target_reference_stale", "target_management_forbidden", "lease_conflict", "lease_stale", "lease_lost", "proxy_protocol_mismatch", "selector_conflict", "plan_expired", "plan_digest_mismatch", "operation_conflict", "operation_revision_stale", "apply_gate_disabled", "validation_failed", "reload_failed", "active_revision_mismatch", "listener_identity_mismatch", "health_failed", "rollback_failed", "reconcile_required", "ambiguous_result":
			return code, http.StatusConflict
		default:
			return "internal_failure", http.StatusInternalServerError
		}
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not found") {
		return "operation_not_found", http.StatusNotFound
	}
	return "internal_failure", http.StatusInternalServerError
}
