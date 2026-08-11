package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/service"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type updateHTTP struct {
	api     *APIHandler
	manager *updateservice.LifecycleManager
}

type updateChannelRequest struct {
	Channel release.Channel `json:"channel"`
}

type updatePrepareRequest struct {
	Channel                release.Channel `json:"channel"`
	ExpectedSequence       uint64          `json:"expectedSequence"`
	ExpectedManifestDigest string          `json:"expectedManifestDigest"`
	IdempotencyKey         string          `json:"idempotencyKey"`
	Confirmation           string          `json:"confirmation"`
	Acknowledged           bool            `json:"acknowledged"`
}

type updateRevisionRequest struct {
	ExpectedRevision uint64 `json:"expectedRevision"`
	Confirmation     string `json:"confirmation"`
}

type updateOperationRevisionRequest struct {
	OperationID      string `json:"operationId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	Confirmation     string `json:"confirmation"`
}

func (a *APIHandler) registerUpdateRoutes(g *gin.RouterGroup) {
	manager := a.Update
	if manager == nil {
		manager = updateservice.SharedLifecycle()
	}
	h := &updateHTTP{api: a, manager: manager}
	group := g.Group("/v1/operations/update")
	group.GET("/capabilities", h.capabilities)
	group.GET("/status", h.status)
	group.GET("/posture", h.status)
	group.GET("/recovery", h.recovery)
	group.POST("/check", h.check)
	group.POST("/prepare", h.prepare)
	group.POST("/preflight", h.prepare)
	group.POST("/activate", h.activateByBody)
	group.POST("/rollback", h.rollbackByBody)
	group.GET("/operations/:operationId", h.operation)
	group.GET("/operations/:operationId/timeline", h.timeline)
	group.POST("/operations/:operationId/activate", h.activate)
	group.POST("/operations/:operationId/rollback", h.rollback)
}

func (h *updateHTTP) capabilities(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, h.manager.Capabilities(c.Request.Context()), nil)
}

func (h *updateHTTP) status(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	channel, ok := updateChannelQuery(c)
	if !ok {
		return
	}
	jsonObj(c, h.manager.Status(c.Request.Context(), channel), nil)
}

func (h *updateHTTP) recovery(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operation, err := h.manager.ActiveOrRecovery(c.Request.Context())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		jsonObj(c, gin.H{"required": false, "operation": nil, "reasonCodes": []string{}}, nil)
		return
	}
	if err != nil {
		h.writeError(c, "update_recovery", err, nil)
		return
	}
	required := updateservice.State(operation.State) == updateservice.StateRecoveryRequired || operation.RestoredUntrusted
	reasonCodes := []string{}
	if operation.ReasonCode != "" {
		reasonCodes = append(reasonCodes, operation.ReasonCode)
	}
	jsonObj(c, gin.H{"required": required, "operation": operation, "reasonCodes": reasonCodes}, nil)
}

func (h *updateHTTP) check(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request updateChannelRequest
	if !decodeSecurityJSON(c, &request) || !validUpdateChannel(request.Channel) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid update channel")
		}
		return
	}
	result, err := h.manager.Check(c.Request.Context(), request.Channel)
	h.api.recordAudit(c, securityContext.Username, "signed_release_checked", "update", service.AuditSeverityInfo,
		map[string]any{"channel": request.Channel, "state": result.State, "sequence": result.Sequence, "signingStatus": result.SigningStatus})
	if err != nil {
		h.writeError(c, "update_check", err, result)
		return
	}
	jsonObj(c, result, nil)
}

func (h *updateHTTP) prepare(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request updatePrepareRequest
	if !decodeSecurityJSON(c, &request) || !validUpdateChannel(request.Channel) || request.ExpectedSequence == 0 ||
		!validDigest(request.ExpectedManifestDigest) || !safeDeploymentID(request.IdempotencyKey, 96) || !request.Acknowledged ||
		request.Confirmation != "PREPARE_UPDATE_"+strconv.FormatUint(request.ExpectedSequence, 10) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid update preparation request")
		}
		return
	}
	target := "release:" + request.ExpectedManifestDigest + ":" + strconv.FormatUint(request.ExpectedSequence, 10)
	if !h.api.requireStepUpAction(c, "update.prepare", target) {
		return
	}
	operation, err := h.manager.Prepare(c.Request.Context(), updateservice.PrepareRequest{Channel: request.Channel,
		ExpectedSequence: request.ExpectedSequence, ExpectedManifestDigest: request.ExpectedManifestDigest,
		IdempotencyKey: request.IdempotencyKey, Acknowledged: true})
	if err != nil {
		h.writeError(c, "update_prepare", err, operation)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "signed_update_prepared", "update", service.AuditSeverityWarn,
		map[string]any{"operationId": operation.OperationID, "state": operation.State, "sequence": operation.Sequence, "revision": operation.Revision})
	jsonObj(c, operation, nil)
}

func (h *updateHTTP) operation(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	id, ok := updateOperationID(c)
	if !ok {
		return
	}
	operation, err := h.manager.Operation(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, "update_operation", err, nil)
		return
	}
	jsonObj(c, operation, nil)
}

func (h *updateHTTP) timeline(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	id, ok := updateOperationID(c)
	if !ok {
		return
	}
	after, limit, ok := boundedTimelineQuery(c)
	if !ok {
		return
	}
	items, truncated, err := h.manager.Timeline(c.Request.Context(), id, after, limit)
	if err != nil {
		h.writeError(c, "update_timeline", err, nil)
		return
	}
	nextAfter := after
	if len(items) > 0 {
		nextAfter = items[len(items)-1].Sequence
	}
	jsonObj(c, gin.H{"items": items, "limit": limit, "after": after, "nextAfter": nextAfter, "truncated": truncated}, nil)
}

func (h *updateHTTP) activate(c *gin.Context) {
	h.mutate(c, "update.activate", "signed_update_activation_requested", true)
}

func (h *updateHTTP) rollback(c *gin.Context) {
	h.mutate(c, "update.rollback", "signed_update_rollback_requested", false)
}

func (h *updateHTTP) activateByBody(c *gin.Context) {
	h.mutateByBody(c, "update.activate", "signed_update_activation_requested", true)
}

func (h *updateHTTP) rollbackByBody(c *gin.Context) {
	h.mutateByBody(c, "update.rollback", "signed_update_rollback_requested", false)
}

func (h *updateHTTP) mutateByBody(c *gin.Context, action, auditEvent string, activate bool) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request updateOperationRevisionRequest
	if !decodeSecurityJSON(c, &request) || !safeUpdateOperationID(request.OperationID) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid update operation request")
		}
		return
	}
	h.mutateOperation(c, securityContext.Username, request.OperationID, request.ExpectedRevision, request.Confirmation, action, auditEvent, activate)
}

func (h *updateHTTP) mutate(c *gin.Context, action, auditEvent string, activate bool) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	id, ok := updateOperationID(c)
	if !ok {
		return
	}
	var request updateRevisionRequest
	if !decodeSecurityJSON(c, &request) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid update operation request")
		}
		return
	}
	h.mutateOperation(c, securityContext.Username, id, request.ExpectedRevision, request.Confirmation, action, auditEvent, activate)
}

func (h *updateHTTP) mutateOperation(c *gin.Context, username, id string, expectedRevision uint64, confirmation, action, auditEvent string, activate bool) {
	current, err := h.manager.Operation(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, strings.ReplaceAll(action, ".", "_"), err, nil)
		return
	}
	wantConfirmation := "ROLLBACK_UPDATE_" + strconv.FormatUint(current.Sequence, 10)
	if activate {
		wantConfirmation = "ACTIVATE_UPDATE_" + strconv.FormatUint(current.Sequence, 10)
	}
	if expectedRevision == 0 || confirmation != wantConfirmation {
		securityBadRequest(c, "invalid update operation request")
		return
	}
	if !h.api.requireStepUpAction(c, action, id+":"+strconv.FormatUint(expectedRevision, 10)) {
		return
	}
	mutation := h.manager.Rollback
	if activate {
		mutation = h.manager.Activate
	}
	operation, err := mutation(c.Request.Context(), updateservice.RevisionRequest{OperationID: id, ExpectedRevision: expectedRevision})
	if err != nil {
		h.writeError(c, strings.ReplaceAll(action, ".", "_"), err, operation)
		return
	}
	h.api.recordAuditSynchronous(c, username, auditEvent, "update", service.AuditSeverityWarn,
		map[string]any{"operationId": id, "state": operation.State, "sequence": operation.Sequence, "revision": operation.Revision})
	jsonObj(c, operation, nil)
}

func (h *updateHTTP) writeError(c *gin.Context, operation string, err error, object any) {
	reason := updateservice.ReasonCode(err)
	state := "REJECTED"
	if errors.Is(err, updateservice.ErrSigningUnavailable) || errors.Is(err, updateservice.ErrProviderUnavailable) {
		state = "UNAVAILABLE"
	}
	if errors.Is(err, updateservice.ErrRecoveryRequired) {
		state = string(updateservice.StateRecoveryRequired)
	}
	c.JSON(http.StatusOK, Msg{Success: false, Msg: reason, Obj: gin.H{"state": state, "operation": operation,
		"reasonCode": reason, "result": object}})
}

func updateChannelQuery(c *gin.Context) (release.Channel, bool) {
	if !strictQueryKeys(c, "invalid update channel", "channel") {
		return "", false
	}
	value := release.Channel(strings.TrimSpace(c.DefaultQuery("channel", string(release.ChannelMain))))
	if !validUpdateChannel(value) {
		securityBadRequest(c, "invalid update channel")
		return "", false
	}
	return value, true
}

func validUpdateChannel(channel release.Channel) bool {
	return channel == release.ChannelMain || channel == release.ChannelBeta
}

func updateOperationID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("operationId"))
	if !safeUpdateOperationID(id) {
		securityBadRequest(c, "invalid update operation id")
		return "", false
	}
	return id, true
}

func safeUpdateOperationID(id string) bool {
	id = strings.TrimSpace(id)
	return safeDeploymentID(id, 96) && strings.HasPrefix(id, "update-operation:")
}

func boundedTimelineQuery(c *gin.Context) (uint64, int, bool) {
	if !strictQueryKeys(c, "invalid update timeline query", "after", "limit") {
		return 0, 0, false
	}
	after := uint64(0)
	limit := 100
	var err error
	if value := strings.TrimSpace(c.Query("after")); value != "" {
		after, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			securityBadRequest(c, "invalid update timeline query")
			return 0, 0, false
		}
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 1 || parsed > 200 {
			securityBadRequest(c, "invalid update timeline query")
			return 0, 0, false
		}
		limit = parsed
	}
	return after, limit, true
}
