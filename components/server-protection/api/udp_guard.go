//go:build !minimal

package api

import (
	"errors"
	"net/http"
	"strings"

	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionudpguard "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/udpguard"
	"github.com/gin-gonic/gin"
)

func (h Handler) udpService() protectionudpguard.Service {
	return h.deps.UDPGuard
}

func (h Handler) udpStatus(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	status, err := h.udpService().Status(c.Request.Context(), queryBool(c, "refresh"))
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	h.deps.JSONObj(c, status, nil)
}

func (h Handler) udpPreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input protectionudpguard.PlanReferenceV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	plan, err := h.udpService().Preview(c.Request.Context(), input)
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	h.audit(c, "server_protection_udp_preview", map[string]any{
		"planId": plan.PlanID, "resourceId": plan.ResourceID, "endpointId": plan.EndpointID, "actualState": plan.ActualState,
	})
	h.deps.JSONObj(c, plan, nil)
}

func (h Handler) udpPrepare(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionudpguard.PrepareRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	result, err := h.udpService().Prepare(c.Request.Context(), h.actor(c), input)
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	if !result.Replayed {
		h.audit(c, "server_protection_udp_prepare", map[string]any{
			"operationId": result.Operation.OperationID, "planId": result.PlanID, "joined": result.Joined,
		})
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) udpApply(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionudpguard.ApplyRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	result, err := h.udpService().Apply(c.Request.Context(), input)
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	if !result.Replayed {
		h.audit(c, "server_protection_udp_apply", map[string]any{
			"operationId": result.Result.OperationID, "planId": input.PlanID, "state": result.Result.State,
			"rollbackAttempted": result.Result.RollbackAttempted,
		})
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) udpRollback(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input protectionudpguard.RollbackRequestV1
	if !decodeStrictJSON(c, &input) {
		return
	}
	result, err := h.udpService().Rollback(c.Request.Context(), input)
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	if !result.Replayed {
		h.audit(c, "server_protection_udp_rollback", map[string]any{
			"operationId": result.Result.OperationID, "state": result.Result.State,
		})
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) udpOperation(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	operation, err := h.udpService().Operation(c.Request.Context(), strings.TrimSpace(c.Param("operationId")))
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	h.deps.JSONObj(c, makeOperationView(operation), nil)
}

func (h Handler) udpRecovery(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	status, err := h.udpService().Recovery(c.Request.Context(), strings.TrimSpace(c.Param("operationId")))
	if err != nil {
		writeUDPServiceError(c, err)
		return
	}
	h.deps.JSONObj(c, status, nil)
}

func writeUDPServiceError(c *gin.Context, err error) {
	if code := protectionudpguard.ErrorCode(err); code != "" {
		status := http.StatusConflict
		switch code {
		case protectionudpguard.CodeMalformedInput, protectionudpguard.CodeConfirmationRequired:
			status = http.StatusBadRequest
		case protectionudpguard.CodeOperationNotFound:
			status = http.StatusNotFound
		case protectionudpguard.CodeInternalFailure:
			status = http.StatusInternalServerError
		}
		writeUDPCode(c, status, code)
		return
	}
	switch {
	case errors.Is(err, protectionoperations.ErrConfirmationRequired):
		writeUDPCode(c, http.StatusBadRequest, protectionudpguard.CodeConfirmationRequired)
	case errors.Is(err, protectionfirewall.ErrPlanRevision):
		writeUDPCode(c, http.StatusConflict, protectionudpguard.CodeRevisionDrift)
	case errors.Is(err, protectionfirewall.ErrMissingCapability):
		writeUDPCode(c, http.StatusConflict, protectionudpguard.CodeMissingCapability)
	case errors.Is(err, protectionfirewall.ErrHealthFailed):
		writeUDPCode(c, http.StatusConflict, "BLOCKED_MISSING_HEALTH")
	case errors.Is(err, protectionfirewall.ErrRollbackHealth):
		writeUDPCode(c, http.StatusConflict, "ROLLBACK_FAILED")
	default:
		writeUDPCode(c, http.StatusConflict, protectionudpguard.CodeInternalFailure)
	}
}

func writeUDPCode(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": code}})
}
