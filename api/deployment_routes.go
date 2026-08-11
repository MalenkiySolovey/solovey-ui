package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/service"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type deploymentHTTP struct {
	api     *APIHandler
	manager *deploymentservice.Manager
}

type deploymentPreviewRequest struct {
	TargetProfile domain.ProfileID `json:"targetProfile"`
	Acknowledged  bool             `json:"acknowledged"`
}

type deploymentStartRequest struct {
	TargetProfile           domain.ProfileID `json:"targetProfile"`
	IdempotencyKey          string           `json:"idempotencyKey"`
	ExpectedPreviewRevision string           `json:"expectedPreviewRevision"`
	ExpectedPostureRevision string           `json:"expectedPostureRevision"`
	Confirmation            string           `json:"confirmation"`
	Acknowledged            bool             `json:"acknowledged"`
}

type deploymentRevisionRequest struct {
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type safeDeploymentOperation struct {
	OperationID       string                `json:"operationId"`
	State             domain.OperationState `json:"state"`
	FromProfile       domain.ProfileID      `json:"fromProfile"`
	TargetProfile     domain.ProfileID      `json:"targetProfile"`
	Revision          uint64                `json:"revision"`
	RestoredUntrusted bool                  `json:"restoredUntrusted"`
	ReconciledAt      int64                 `json:"reconciledAt,omitempty"`
	CreatedAt         int64                 `json:"createdAt"`
	UpdatedAt         int64                 `json:"updatedAt"`
	Reasons           []string              `json:"reasonCodes,omitempty"`
	RollbackAvailable bool                  `json:"rollbackAvailable"`
}

type safeDeploymentTimeline struct {
	Sequence  uint64 `json:"sequence"`
	State     string `json:"state"`
	Event     string `json:"event"`
	Reason    string `json:"reasonCode,omitempty"`
	Revision  string `json:"revision"`
	CreatedAt int64  `json:"createdAt"`
}

func (a *APIHandler) registerDeploymentRoutes(g *gin.RouterGroup) {
	manager := a.Deployment
	if manager == nil {
		manager = deploymentservice.Shared()
	}
	h := &deploymentHTTP{api: a, manager: manager}
	group := g.Group("/v1/operations/deployment")
	group.GET("/profiles", h.profiles)
	group.GET("/manifests", h.manifests)
	group.GET("/broker", h.broker)
	group.GET("/capabilities", h.capabilities)
	group.GET("/status", h.status)
	group.GET("/posture", h.status)
	group.GET("/doctor", h.doctor)
	group.GET("/recovery", h.recovery)
	group.POST("/preview", h.preview)
	group.POST("/migration", h.start)
	group.GET("/migration/:operationId", h.operation)
	group.GET("/migration/:operationId/status", h.operation)
	group.GET("/migration/:operationId/timeline", h.timeline)
	group.POST("/migration/:operationId/confirm", h.confirm)
	group.POST("/migration/:operationId/rollback", h.rollback)
}

func (h *deploymentHTTP) profiles(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	profiles := domain.Catalog()
	jsonObj(c, gin.H{"schema": domain.SchemaV1, "items": profiles, "revision": domain.Revision(profiles)}, nil)
}

func (h *deploymentHTTP) manifests(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	profiles := domain.Catalog()
	items := make([]gin.H, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, gin.H{"profile": profile.ID, "runtime": profile.Runtime, "support": profile.Support,
			"generatedRevision": profile.Revision, "freshInstallDefault": profile.FreshInstallDefault})
	}
	jsonObj(c, gin.H{"schema": domain.SchemaV1, "items": items, "rawConfigurationAccepted": false, "revision": domain.Revision(items)}, nil)
}

func (h *deploymentHTTP) broker(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	capabilities := h.manager.Capabilities(c.Request.Context())
	jsonObj(c, gin.H{"available": capabilities.Observe == domain.Available, "protocolRevision": domain.ProviderV1,
		"transport": "systemd-activated-unix-peer-credentials", "peerPosture": "root-owned-pinned-client-manifest",
		"capabilities": capabilities}, nil)
}

func (h *deploymentHTTP) capabilities(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	jsonObj(c, h.manager.Capabilities(c.Request.Context()), nil)
}

func (h *deploymentHTTP) status(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	posture, err := h.manager.Status(c.Request.Context())
	if err != nil {
		h.writeReadError(c, "deployment_status", err)
		return
	}
	state, stateErr := h.manager.Repository.State(c.Request.Context())
	if stateErr != nil {
		h.writeReadError(c, "deployment_status", stateErr)
		return
	}
	statusState := "VERIFIED"
	switch {
	case state.GeneratedProfile != "" && state.GeneratedProfile != state.InstalledProfile:
		statusState = "GENERATED_NOT_INSTALLED"
	case state.InstalledProfile != "" && state.InstalledProfile != state.ActiveProfile:
		statusState = "INSTALLED_NOT_ACTIVE"
	case !state.Trusted || state.ActiveProfile == "" || state.ActiveProfile != state.VerifiedProfile:
		statusState = "ACTIVE_NOT_VERIFIED"
	}
	evidence := "NORMAL_CI_VERIFIED_LIVE_NOT_RUN"
	if profile, ok := domain.Lookup(posture.Profile); ok {
		evidence = profile.EvidenceStatus
	}
	jsonObj(c, gin.H{"state": statusState, "posture": posture, "desiredProfile": state.DesiredProfile,
		"generatedProfile": state.GeneratedProfile, "generatedRevision": state.GeneratedRevision,
		"installedProfile": state.InstalledProfile, "activeProfile": state.ActiveProfile,
		"verifiedProfile": state.VerifiedProfile, "compatibilityState": state.CompatibilityState,
		"doctorRevision": state.DoctorRevision, "trusted": state.Trusted,
		"evidenceStatus": evidence}, nil)
}

func (h *deploymentHTTP) doctor(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	report, err := h.manager.Doctor(c.Request.Context())
	if err != nil {
		h.writeReadError(c, "deployment_doctor", err)
		return
	}
	jsonObj(c, report, nil)
}

func (h *deploymentHTTP) recovery(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operation, err := h.manager.Repository.Active(c.Request.Context())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		operation, err = h.manager.Repository.Recovery(c.Request.Context())
		if errors.Is(err, gorm.ErrRecordNotFound) {
			jsonObj(c, gin.H{"required": false, "operation": nil, "reasonCodes": []string{}}, nil)
			return
		}
	}
	if err != nil {
		h.writeReadError(c, "deployment_recovery", err)
		return
	}
	jsonObj(c, gin.H{"required": operation.State == domain.StateManualRecoveryRequired || operation.RestoredUntrusted,
		"operation": publicDeploymentOperation(operation)}, nil)
}

func (h *deploymentHTTP) preview(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request deploymentPreviewRequest
	if !decodeSecurityJSON(c, &request) || !validTargetProfile(request.TargetProfile) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid deployment preview")
		}
		return
	}
	preview, err := h.manager.Preview(c.Request.Context(), request.TargetProfile, request.Acknowledged)
	if err != nil {
		h.writeReadError(c, "deployment_preview", err)
		return
	}
	h.api.recordAudit(c, securityContext.Username, "deployment_previewed", "deployment", service.AuditSeverityInfo,
		map[string]any{"targetProfile": request.TargetProfile, "possible": preview.Possible, "revision": preview.Revision})
	jsonObj(c, preview, nil)
}

func (h *deploymentHTTP) start(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request deploymentStartRequest
	if !decodeSecurityJSON(c, &request) {
		return
	}
	if !validTargetProfile(request.TargetProfile) || !safeDeploymentID(request.IdempotencyKey, 96) ||
		!validDigest(request.ExpectedPreviewRevision) || !validDigest(request.ExpectedPostureRevision) ||
		request.Confirmation != deploymentConfirmation(request.TargetProfile) || !request.Acknowledged {
		securityBadRequest(c, "invalid deployment migration request")
		return
	}
	if !h.api.requireStepUpAction(c, "deployment.migrate", "deployment-profile:"+string(request.TargetProfile)+":"+request.ExpectedPreviewRevision) {
		return
	}
	operation, err := h.manager.Start(c.Request.Context(), deploymentservice.StartRequest{TargetProfile: request.TargetProfile,
		IdempotencyKey: request.IdempotencyKey, ExpectedPreviewRevision: request.ExpectedPreviewRevision,
		ExpectedPostureRevision: request.ExpectedPostureRevision, Acknowledged: true})
	if err != nil {
		h.writeMutationError(c, "deployment_migration", err, operation)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "deployment_migration_started", "deployment", service.AuditSeverityWarn,
		map[string]any{"operationId": operation.OperationID, "targetProfile": operation.TargetProfile, "state": operation.State, "revision": operation.Revision})
	jsonObj(c, publicDeploymentOperation(operation), nil)
}

func (h *deploymentHTTP) operation(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	id, ok := deploymentOperationID(c)
	if !ok {
		return
	}
	operation, err := h.manager.Operation(c.Request.Context(), id)
	if err != nil {
		h.writeReadError(c, "deployment_operation", err)
		return
	}
	jsonObj(c, publicDeploymentOperation(operation), nil)
}

func (h *deploymentHTTP) timeline(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	id, ok := deploymentOperationID(c)
	if !ok {
		return
	}
	rows, err := h.manager.Timeline(c.Request.Context(), id)
	if err != nil {
		h.writeReadError(c, "deployment_timeline", err)
		return
	}
	items := make([]safeDeploymentTimeline, 0, len(rows))
	for _, row := range rows {
		items = append(items, publicDeploymentTimeline(row))
	}
	jsonObj(c, gin.H{"items": items, "revision": domain.Revision(items)}, nil)
}

func (h *deploymentHTTP) confirm(c *gin.Context) {
	h.mutateExisting(c, "deployment.confirm", "deployment_migration_confirmed", h.manager.Confirm)
}

func (h *deploymentHTTP) rollback(c *gin.Context) {
	h.mutateExisting(c, "deployment.rollback", "deployment_migration_rollback_requested", h.manager.Rollback)
}

func (h *deploymentHTTP) mutateExisting(c *gin.Context, action, auditEvent string,
	mutation func(context.Context, deploymentservice.ConfirmRequest) (domain.Operation, error)) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	id, ok := deploymentOperationID(c)
	if !ok {
		return
	}
	var request deploymentRevisionRequest
	if !decodeSecurityJSON(c, &request) || request.ExpectedRevision == 0 {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid deployment operation request")
		}
		return
	}
	if !h.api.requireStepUpAction(c, action, "deployment-operation:"+id+":"+uintString(request.ExpectedRevision)) {
		return
	}
	operation, err := mutation(c.Request.Context(), deploymentservice.ConfirmRequest{OperationID: id, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		h.writeMutationError(c, strings.ReplaceAll(action, ".", "_"), err, operation)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, auditEvent, "deployment", service.AuditSeverityWarn,
		map[string]any{"operationId": id, "state": operation.State, "revision": operation.Revision})
	jsonObj(c, publicDeploymentOperation(operation), nil)
}

func (h *deploymentHTTP) writeReadError(c *gin.Context, operation string, err error) {
	code := deploymentReasonCode(err)
	state := "UNAVAILABLE"
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state = "NOT_OBSERVED"
	}
	c.JSON(http.StatusOK, Msg{Success: false, Msg: code, Obj: gin.H{"state": state, "operation": operation, "reasonCode": code}})
}

func (h *deploymentHTTP) writeMutationError(c *gin.Context, operation string, err error, result domain.Operation) {
	code := deploymentReasonCode(err)
	state := "REJECTED"
	if errors.Is(err, deploymentservice.ErrProviderUnavailable) {
		state = "UNAVAILABLE"
	}
	var public any
	if result.OperationID != "" {
		public = publicDeploymentOperation(result)
	}
	c.JSON(http.StatusOK, Msg{Success: false, Msg: code, Obj: gin.H{"state": state, "operation": operation,
		"reasonCode": code, "migration": public}})
}

func deploymentReasonCode(err error) string {
	switch {
	case errors.Is(err, deploymentservice.ErrProviderUnavailable):
		return "deployment_provider_unavailable"
	case errors.Is(err, deploymentservice.ErrRevisionMismatch):
		return "deployment_revision_mismatch"
	case errors.Is(err, deploymentservice.ErrOperationConflict):
		return "deployment_operation_conflict"
	case errors.Is(err, deploymentservice.ErrUnsafeMigration):
		return "deployment_manual_recovery_required"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "deployment_not_observed"
	default:
		return "deployment_internal_error"
	}
}

func publicDeploymentOperation(operation domain.Operation) safeDeploymentOperation {
	return safeDeploymentOperation{OperationID: operation.OperationID, State: operation.State, FromProfile: operation.FromProfile,
		TargetProfile: operation.TargetProfile, Revision: operation.Revision, RestoredUntrusted: operation.RestoredUntrusted,
		ReconciledAt: operation.ReconciledAt, CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt,
		Reasons: append([]string(nil), operation.Reasons...), RollbackAvailable: operation.CheckpointRef != "" && !operation.State.Terminal()}
}

func publicDeploymentTimeline(row model.DeploymentJournal) safeDeploymentTimeline {
	return safeDeploymentTimeline{Sequence: row.Sequence, State: row.State, Event: row.Event, Reason: row.Reason,
		Revision: row.Revision, CreatedAt: row.CreatedAt}
}

func deploymentOperationID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("operationId"))
	if !safeDeploymentID(id, 96) || !strings.HasPrefix(id, "deployment-operation:") {
		securityBadRequest(c, "invalid deployment operation id")
		return "", false
	}
	return id, true
}

func safeDeploymentID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func validTargetProfile(profile domain.ProfileID) bool {
	return profile == domain.NativeHardened || profile == domain.NativeNetworkAdvanced
}

func deploymentConfirmation(profile domain.ProfileID) string {
	return "MIGRATE_TO_" + strings.ToUpper(strings.ReplaceAll(string(profile), "-", "_"))
}
