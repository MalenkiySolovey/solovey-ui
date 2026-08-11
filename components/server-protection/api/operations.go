//go:build !minimal

package api

import (
	"errors"
	"net/http"
	"strings"

	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/gin-gonic/gin"
)

func (h Handler) operations(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	if h.deps.Operations == nil {
		writeError(c, http.StatusServiceUnavailable, "operation_service_unavailable", nil)
		return
	}
	items, err := h.deps.Operations.List(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	artifacts, err := h.deps.Repository.ListArtifacts(c.Request.Context())
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	bundleAvailable := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Scope == "recovery" {
			bundleAvailable[artifact.OperationID] = true
		}
	}
	recoveryRequired := 0
	views := make([]operationView, 0, len(items))
	for _, item := range items {
		if item.State == protectionoperations.StateApplying || item.State == protectionoperations.StateHealthFailed || item.State == protectionoperations.StateRollingBack || item.State == protectionoperations.StateRollbackFailed || item.State == protectionoperations.StateLockSuspect {
			recoveryRequired++
		}
		view := makeOperationView(item)
		view.RecoveryBundleAvailable = bundleAvailable[item.OperationID]
		views = append(views, view)
	}
	h.deps.JSONObj(c, gin.H{
		"items": views, "recoveryRequired": recoveryRequired,
		"confirmationTemplates": gin.H{
			"prepare":     "PREPARE SERVER PROTECTION <revision>",
			"apply":       "APPLY SERVER PROTECTION <operation-id>",
			"rollback":    "ROLLBACK SERVER PROTECTION <operation-id>",
			"forceUnlock": "FORCE UNLOCK <operation-id>",
			"forgetState": "FORGET_SERVER_PROTECTION_STATE",
		},
	}, nil)
}

type operationView struct {
	OperationID             string `json:"operationId"`
	Kind                    string `json:"kind"`
	ResourceID              string `json:"resourceId,omitempty"`
	Protocol                string `json:"protocol,omitempty"`
	Listen                  string `json:"listen,omitempty"`
	Port                    *int   `json:"port,omitempty"`
	State                   string `json:"state"`
	Revision                int    `json:"revision"`
	PlanRevision            string `json:"planRevision,omitempty"`
	RecoveryAttempts        int    `json:"recoveryAttempts"`
	RecoveryErrorCode       string `json:"recoveryErrorCode,omitempty"`
	RecoveryBundleAvailable bool   `json:"recoveryBundleAvailable"`
	CreatedAt               int64  `json:"createdAt"`
	UpdatedAt               int64  `json:"updatedAt"`
}

func makeOperationView(item protectionrepository.OperationLockModel) operationView {
	return operationView{
		OperationID: item.OperationID, Kind: item.Kind, ResourceID: item.ResourceID,
		Protocol: item.Protocol, Listen: item.Listen, Port: item.Port, State: item.State,
		Revision: item.Revision, RecoveryAttempts: item.RecoveryAttempts,
		PlanRevision:      item.PlanRevision,
		RecoveryErrorCode: item.RecoveryErrorCode, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

type prepareOperationInput struct {
	Kind           string `json:"kind"`
	ResourceID     string `json:"resourceId"`
	Protocol       string `json:"protocol"`
	Listen         string `json:"listen"`
	Port           *int   `json:"port"`
	PlanRevision   string `json:"planRevision"`
	IdempotencyKey string `json:"idempotencyKey"`
	Confirmation   string `json:"confirmation"`
}

func (h Handler) prepareOperation(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	if h.deps.Operations == nil {
		writeError(c, http.StatusServiceUnavailable, "operation_service_unavailable", nil)
		return
	}
	var input prepareOperationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	result, err := h.deps.Operations.Prepare(c.Request.Context(), protectionoperations.PrepareRequest{
		PlanRevision: strings.TrimSpace(input.PlanRevision), Confirmation: input.Confirmation,
		Acquire: protectionoperations.AcquireRequest{
			Kind: strings.TrimSpace(input.Kind), ResourceID: strings.TrimSpace(input.ResourceID), Protocol: strings.TrimSpace(input.Protocol),
			Listen: strings.TrimSpace(input.Listen), Port: input.Port, IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Actor: h.actor(c),
		},
	})
	writeOperationError(c, result, err, h.deps.JSONObj)
}

type confirmOperationInput struct {
	OperationID  string `json:"operationId"`
	Confirmation string `json:"confirmation"`
}

type firewallPrepareInput struct {
	PlanRevision   string `json:"planRevision"`
	IdempotencyKey string `json:"idempotencyKey"`
	Confirmation   string `json:"confirmation"`
}

func (h Handler) firewallPrepare(c *gin.Context) {
	if !h.applyEnabled(c) {
		h.audit(c, "server_protection.firewall.prepare", map[string]any{"phase": "capability_denied"})
		return
	}
	if h.deps.Firewall == nil {
		writeError(c, http.StatusConflict, "missing_capability", nil)
		return
	}
	var input firewallPrepareInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	plan, _, err := h.currentFirewallPlan(c, true)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	if strings.TrimSpace(input.PlanRevision) != plan.Revision {
		writeError(c, http.StatusConflict, "revision_conflict", nil)
		return
	}
	result, err := h.deps.Firewall.Prepare(c.Request.Context(), protectionfirewall.PrepareInput{Plan: plan, Actor: h.actor(c), IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Confirmation: input.Confirmation})
	if err != nil {
		writeFirewallWorkflowError(c, err)
		return
	}
	h.audit(c, "server_protection_firewall_prepare", map[string]any{"operationId": result.Operation.OperationID, "joined": result.Joined})
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) firewallApply(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input confirmOperationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	if input.Confirmation != "APPLY SERVER PROTECTION "+strings.TrimSpace(input.OperationID) {
		writeError(c, http.StatusBadRequest, "confirmation_required", protectionoperations.ErrConfirmationRequired)
		return
	}
	if !h.applyConfigured(c) {
		h.audit(c, "server_protection.apply", map[string]any{"operationId": strings.TrimSpace(input.OperationID), "phase": "capability_denied"})
		return
	}
	if h.deps.Firewall == nil {
		h.audit(c, "server_protection.apply", map[string]any{"operationId": strings.TrimSpace(input.OperationID), "phase": "missing_capability"})
		writeError(c, http.StatusConflict, "missing_capability", nil)
		return
	}
	plan, resources, err := h.currentFirewallPlan(c, true)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	result, err := h.deps.Firewall.Apply(c.Request.Context(), protectionfirewall.ApplyInput{OperationID: strings.TrimSpace(input.OperationID), Plan: plan, Resources: resources, Confirmation: input.Confirmation})
	h.audit(c, "server_protection_firewall_apply", map[string]any{"operationId": result.OperationID, "state": result.State, "rollbackAttempted": result.RollbackAttempted})
	if err != nil {
		writeFirewallWorkflowError(c, err)
		return
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) firewallRollback(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	var input confirmOperationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	if input.Confirmation != "ROLLBACK SERVER PROTECTION "+strings.TrimSpace(input.OperationID) {
		writeError(c, http.StatusBadRequest, "confirmation_required", protectionoperations.ErrConfirmationRequired)
		return
	}
	if !h.applyConfigured(c) {
		h.audit(c, "server_protection.rollback", map[string]any{"operationId": strings.TrimSpace(input.OperationID), "phase": "capability_denied"})
		return
	}
	if h.deps.Firewall == nil {
		h.audit(c, "server_protection.rollback", map[string]any{"operationId": strings.TrimSpace(input.OperationID), "phase": "missing_capability"})
		writeError(c, http.StatusConflict, "missing_capability", nil)
		return
	}
	result, err := h.deps.Firewall.Rollback(c.Request.Context(), strings.TrimSpace(input.OperationID), input.Confirmation)
	h.audit(c, "server_protection_firewall_rollback", map[string]any{"operationId": result.OperationID, "state": result.State})
	if err != nil {
		writeFirewallWorkflowError(c, err)
		return
	}
	h.deps.JSONObj(c, result, nil)
}

func (h Handler) confirmUnavailable(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.applyAllowed(c) {
			return
		}
		if h.deps.Operations == nil {
			writeError(c, http.StatusServiceUnavailable, "operation_service_unavailable", nil)
			return
		}
		var input confirmOperationInput
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "validation_error", err)
			return
		}
		err := h.deps.Operations.ConfirmUnavailableAction(c.Request.Context(), protectionoperations.ConfirmActionRequest{
			OperationID: strings.TrimSpace(input.OperationID), Action: action, Actor: h.actor(c), Confirmation: input.Confirmation,
		})
		switch {
		case errors.Is(err, protectionoperations.ErrConfirmationRequired):
			writeError(c, http.StatusBadRequest, "confirmation_required", err)
		case errors.Is(err, protectionoperations.ErrCapabilityUnavailable):
			writeError(c, http.StatusConflict, "missing_capability", err)
		case errors.Is(err, protectionrepository.ErrRecordNotFound):
			writeError(c, http.StatusNotFound, "not_found", err)
		default:
			h.deps.JSONObj(c, nil, err)
		}
	}
}

type forgetStateInput struct {
	Revision     int    `json:"revision"`
	Confirmation string `json:"confirmation"`
}

func (h Handler) forgetState(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	if h.deps.Operations == nil {
		writeError(c, http.StatusServiceUnavailable, "operation_service_unavailable", nil)
		return
	}
	var input forgetStateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	item, err := h.deps.Operations.ForgetState(c.Request.Context(), protectionoperations.ForgetStateRequest{
		OperationID: strings.TrimSpace(c.Param("operationId")), Revision: input.Revision, Actor: h.actor(c), Confirmation: input.Confirmation,
	})
	switch {
	case errors.Is(err, protectionoperations.ErrConfirmationRequired):
		writeError(c, http.StatusBadRequest, "confirmation_required", err)
	case errors.Is(err, protectionoperations.ErrConflict):
		writeError(c, http.StatusConflict, "non_terminal_operation", err)
	case errors.Is(err, protectionoperations.ErrRevisionConflict):
		writeError(c, http.StatusConflict, "revision_conflict", err)
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "not_found", err)
	default:
		h.deps.JSONObj(c, item, err)
	}
}

func writeOperationError(c *gin.Context, value any, err error, write func(*gin.Context, interface{}, error)) {
	switch {
	case errors.Is(err, protectionoperations.ErrConfirmationRequired):
		writeError(c, http.StatusBadRequest, "confirmation_required", err)
	case errors.Is(err, protectionoperations.ErrConflict):
		writeError(c, http.StatusConflict, "operation_conflict", err)
	case err != nil:
		write(c, nil, err)
	default:
		write(c, value, nil)
	}
}

type forceUnlockInput struct {
	Revision     int    `json:"revision"`
	Confirmation string `json:"confirmation"`
}

func (h Handler) forceUnlock(c *gin.Context) {
	if !h.applyAllowed(c) {
		return
	}
	if h.deps.Operations == nil {
		writeError(c, http.StatusServiceUnavailable, "operation_service_unavailable", nil)
		return
	}
	operationID := strings.TrimSpace(c.Param("operationId"))
	var input forceUnlockInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	confirmed := input.Confirmation == "FORCE UNLOCK "+operationID
	item, err := h.deps.Operations.ForceUnlock(c.Request.Context(), protectionoperations.ForceUnlockRequest{
		OperationID: operationID, Revision: input.Revision, Actor: h.actor(c), Confirmed: confirmed,
	})
	switch {
	case errors.Is(err, protectionoperations.ErrConfirmationRequired):
		writeError(c, http.StatusBadRequest, "confirmation_required", err)
	case errors.Is(err, protectionoperations.ErrRevisionConflict):
		writeError(c, http.StatusConflict, "revision_conflict", err)
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "not_found", err)
	case err != nil:
		h.deps.JSONObj(c, nil, err)
	default:
		h.deps.JSONObj(c, item, nil)
	}
}
