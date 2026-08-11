package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	sshmanagementservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type sshManagementHTTP struct {
	api     *APIHandler
	manager *sshmanagementservice.Manager
}

func (a *APIHandler) registerSSHManagementRoutes(g *gin.RouterGroup) {
	manager := a.SSHManagement
	if manager == nil {
		manager = sshmanagementservice.Shared()
	}
	h := &sshManagementHTTP{api: a, manager: manager}
	group := g.Group("/v1/operations/ssh")
	group.GET("/posture", h.posture)
	group.GET("/capabilities", h.capabilities)
	group.GET("/endpoints", h.endpoints)
	group.GET("/recovery", h.recovery)
	group.GET("/desired", h.desired)
	group.POST("/preview", h.preview)
	group.POST("/candidate", h.startCandidate)
	group.GET("/candidate/:operationId", h.candidate)
	group.GET("/candidate/:operationId/status", h.candidate)
	group.GET("/candidate/:operationId/timeline", h.timeline)
	group.GET("/candidate/:operationId/reconnect", h.reconnectState)
	group.POST("/candidate/:operationId/reconnect/confirm", h.confirmReconnect)
	group.POST("/candidate/:operationId/rollback", h.rollback)
}

func (h *sshManagementHTTP) posture(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	posture, err := h.manager.LatestPosture(c.Request.Context())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		jsonObj(c, gin.H{"state": "UNAVAILABLE", "fresh": false, "reasonCodes": []string{string(domain.ReasonProviderUnavailable)}, "posture": nil}, nil)
		return
	}
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	now := time.Now().UTC()
	jsonObj(c, gin.H{"state": "OBSERVED", "fresh": posture.Validate(now) == nil, "posture": posture}, nil)
}

func (h *sshManagementHTTP) capabilities(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, h.manager.Capabilities(c.Request.Context()), nil)
}

func (h *sshManagementHTTP) endpoints(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	items := h.manager.EndpointSnapshot(c.Request.Context())
	jsonObj(c, gin.H{"items": items, "revision": domain.Revision(items)}, nil)
}

func (h *sshManagementHTTP) recovery(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	value := h.manager.RecoverySnapshot(c.Request.Context())
	jsonObj(c, gin.H{"items": value.Paths, "reasonCodes": value.ReasonCodes, "generatedAt": value.GeneratedAt, "revision": domain.Revision(value.Paths)}, nil)
}

func (h *sshManagementHTTP) desired(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, gin.H{"schema": domain.PolicySchemaV1, "fields": []gin.H{
		{"id": "maxAuthTries", "kind": "integer", "minimum": 1, "maximum": 20},
		{"id": "loginGraceTimeSeconds", "kind": "integer", "minimum": 1, "maximum": 600},
		{"id": "passwordAuthentication", "kind": "boolean"},
		{"id": "kbdInteractiveAuthentication", "kind": "boolean"},
		{"id": "permitRootLogin", "kind": "enum", "values": []domain.RootLoginPolicy{domain.RootLoginUnchanged, domain.RootLoginYes, domain.RootLoginNo, domain.RootLoginProhibitPassword}},
		{"id": "pubkeyAuthentication", "kind": "boolean"},
	}, "rawConfigurationAccepted": false}, nil)
}

func (h *sshManagementHTTP) preview(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request sshmanagementservice.PreviewRequestV1
	if !decodeSecurityJSON(c, &request) {
		return
	}
	preview, err := h.manager.Preview(c.Request.Context(), request)
	if err == nil {
		h.api.recordAudit(c, securityContext.Username, "ssh_management_previewed", "ssh-management", "info", map[string]any{"possible": preview.Possible, "revision": preview.Revision})
	}
	jsonObj(c, preview, err)
}

func (h *sshManagementHTTP) startCandidate(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request sshmanagementservice.StartRequestV1
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !h.api.requireStepUpAction(c, "ssh.candidate.apply", "ssh-candidate:"+request.ExpectedPreviewRevision) {
		return
	}
	result, err := h.manager.Start(c.Request.Context(), request)
	if err != nil {
		h.writeMutationError(c, "ssh_candidate_start", err)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "ssh_management_candidate_started", "ssh-management", "warn", map[string]any{
		"operationId": result.Candidate.OperationID, "state": result.Candidate.State, "revision": result.Candidate.Revision,
	})
	// The verifier is deliberately absent. The production proof helper delivers
	// it only to the newly authenticated SSH terminal; it is never browser state.
	jsonObj(c, result.Candidate, nil)
}

func (h *sshManagementHTTP) candidate(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operationID, ok := sshOperationID(c)
	if !ok {
		return
	}
	candidate, err := h.manager.Candidate(c.Request.Context(), operationID)
	jsonObj(c, candidate, err)
}

func (h *sshManagementHTTP) reconnectState(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operationID, ok := sshOperationID(c)
	if !ok {
		return
	}
	state, err := h.manager.ReconnectState(c.Request.Context(), operationID)
	jsonObj(c, state, err)
}

func (h *sshManagementHTTP) timeline(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operationID, ok := sshOperationID(c)
	if !ok {
		return
	}
	items, err := h.manager.Timeline(c.Request.Context(), operationID)
	jsonObj(c, gin.H{"items": items}, err)
}

type reconnectConfirmationRequest struct {
	ExpectedRevision    uint64 `json:"expectedRevision"`
	ProviderEvidenceRef string `json:"providerEvidenceRef"`
}

func (h *sshManagementHTTP) confirmReconnect(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	operationID, validOperation := sshOperationID(c)
	if !validOperation {
		return
	}
	var request reconnectConfirmationRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if request.ExpectedRevision == 0 ||
		!safeDeploymentID(request.ProviderEvidenceRef, 128) || !strings.HasPrefix(request.ProviderEvidenceRef, "ssh-proof:") {
		securityBadRequest(c, "invalid reconnect confirmation")
		return
	}
	if !h.api.requireStepUpAction(c, "ssh.candidate.confirm", "ssh-operation:"+operationID+":"+uintString(request.ExpectedRevision)) {
		return
	}
	candidate, err := h.manager.Confirm(c.Request.Context(), sshmanagementservice.ConfirmRequestV1{OperationID: operationID, ExpectedRevision: request.ExpectedRevision, ProviderEvidenceRef: request.ProviderEvidenceRef})
	if err != nil {
		h.writeMutationError(c, "ssh_reconnect_confirm", err)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "ssh_management_reconnect_confirmed", "ssh-management", "warn", map[string]any{"operationId": operationID, "revision": candidate.Revision})
	jsonObj(c, candidate, nil)
}

type sshRollbackRequest struct {
	ExpectedRevision uint64 `json:"expectedRevision"`
}

func (h *sshManagementHTTP) rollback(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	operationID, validOperation := sshOperationID(c)
	if !validOperation {
		return
	}
	var request sshRollbackRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if request.ExpectedRevision == 0 {
		securityBadRequest(c, "invalid rollback request")
		return
	}
	if !h.api.requireStepUpAction(c, "ssh.candidate.rollback", "ssh-operation:"+operationID+":"+uintString(request.ExpectedRevision)) {
		return
	}
	candidate, err := h.manager.Rollback(c.Request.Context(), sshmanagementservice.RollbackRequestV1{OperationID: operationID, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		h.writeMutationError(c, "ssh_candidate_rollback", err)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "ssh_management_rollback_requested", "ssh-management", "warn", map[string]any{"operationId": operationID, "state": candidate.State, "revision": candidate.Revision})
	jsonObj(c, candidate, nil)
}

func (h *sshManagementHTTP) writeMutationError(c *gin.Context, operation string, err error) {
	code := domain.ErrorCode(err)
	state := "REJECTED"
	if code == domain.ReasonProviderUnavailable || code == domain.ReasonProductionMutationAbsent {
		state = "UNAVAILABLE"
	}
	c.JSON(http.StatusOK, Msg{Success: false, Msg: string(code), Obj: gin.H{"state": state, "operation": operation, "reasonCode": code}})
}

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}

func sshOperationID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("operationId"))
	if !safeSSHOperationID(id) {
		securityBadRequest(c, "invalid SSH operation id")
		return "", false
	}
	return id, true
}

func safeSSHOperationID(id string) bool {
	id = strings.TrimSpace(id)
	return len(id) <= 64 && safeDeploymentID(id, 64) && strings.HasPrefix(id, "ssh-operation:")
}
