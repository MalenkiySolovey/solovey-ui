package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type dataLifecycleHTTP struct {
	api     *APIHandler
	manager *datalifecycle.Manager
}

type dropDataPreviewRequest struct {
	OwnerID string `json:"ownerId"`
}

type dropDataExecuteRequest struct {
	OwnerID                 string `json:"ownerId"`
	ExpectedPreviewRevision string `json:"expectedPreviewRevision"`
	IdempotencyKey          string `json:"idempotencyKey"`
	Confirmation            string `json:"confirmation"`
	BackupAcknowledged      bool   `json:"backupAcknowledged"`
	Acknowledged            bool   `json:"acknowledged"`
}

func (a *APIHandler) registerDataLifecycleRoutes(g *gin.RouterGroup) {
	manager := a.DataLifecycle
	if manager == nil {
		manager = datalifecycle.Shared()
	}
	h := &dataLifecycleHTTP{api: a, manager: manager}
	group := g.Group("/v1/operations/data")
	group.POST("/drop/preview", h.dropPreview)
	group.POST("/drop", h.dropExecute)
	group.GET("/operations/:operationId", h.operation)
	group.GET("/recovery", h.recovery)
}

func (h *dataLifecycleHTTP) dropPreview(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	var request dropDataPreviewRequest
	if !decodeSecurityJSON(c, &request) || !safeOwnerID(request.OwnerID) {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid Drop Data preview request")
		}
		return
	}
	preview, err := h.manager.Preview(c.Request.Context(), request.OwnerID)
	if err != nil {
		h.writeError(c, "drop_data_preview", err, nil)
		return
	}
	jsonObj(c, preview, nil)
}

func (h *dataLifecycleHTTP) dropExecute(c *gin.Context) {
	securityContext, ok := requireAuthenticatedSecurityContext(c)
	if !ok {
		return
	}
	var request dropDataExecuteRequest
	if !decodeSecurityJSON(c, &request) || !safeOwnerID(request.OwnerID) || !validDigest(request.ExpectedPreviewRevision) ||
		!safeDeploymentID(request.IdempotencyKey, 96) || request.Confirmation != dropDataConfirmation(request.OwnerID) ||
		!request.BackupAcknowledged || !request.Acknowledged {
		if !c.IsAborted() {
			securityBadRequest(c, "invalid Drop Data execution request")
		}
		return
	}
	target := "durable-owner:" + request.OwnerID + ":" + request.ExpectedPreviewRevision
	if !h.api.requireStepUpAction(c, "data.drop", target) {
		return
	}
	operation, err := h.manager.Execute(c.Request.Context(), datalifecycle.ExecuteRequest{OwnerID: request.OwnerID,
		ExpectedPreviewRevision: request.ExpectedPreviewRevision, IdempotencyKey: request.IdempotencyKey,
		Confirmation: request.Confirmation, BackupAcknowledged: true})
	if err != nil {
		h.writeError(c, "drop_data_execute", err, operation)
		return
	}
	h.api.recordAuditSynchronous(c, securityContext.Username, "component_data_dropped", "data-lifecycle", service.AuditSeverityWarn,
		map[string]any{"operationId": operation.OperationID, "ownerId": operation.OwnerID, "state": operation.State, "revision": operation.Revision})
	jsonObj(c, operation, nil)
}

func (h *dataLifecycleHTTP) operation(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	id := strings.TrimSpace(c.Param("operationId"))
	if !safeDeploymentID(id, 96) || !strings.HasPrefix(id, "data-operation:") {
		securityBadRequest(c, "invalid data lifecycle operation id")
		return
	}
	operation, err := h.manager.Operation(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, "data_lifecycle_operation", err, nil)
		return
	}
	jsonObj(c, operation, nil)
}

func (h *dataLifecycleHTTP) recovery(c *gin.Context) {
	if _, ok := requireAuthenticatedSecurityContext(c); !ok {
		return
	}
	operation, err := h.manager.Recovery(c.Request.Context())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		jsonObj(c, gin.H{"required": false, "operation": nil, "reasonCodes": []string{}}, nil)
		return
	}
	if err != nil {
		h.writeError(c, "data_lifecycle_recovery", err, nil)
		return
	}
	jsonObj(c, gin.H{"required": true, "operation": operation, "reasonCodes": []string{operation.ReasonCode}}, nil)
}

func (h *dataLifecycleHTTP) writeError(c *gin.Context, operation string, err error, result any) {
	reason := datalifecycle.ReasonCode(err)
	state := "REJECTED"
	if errors.Is(err, datalifecycle.ErrRecoveryRequired) {
		state = "RECOVERY_REQUIRED"
	}
	c.JSON(http.StatusOK, Msg{Success: false, Msg: reason, Obj: gin.H{"state": state, "operation": operation,
		"reasonCode": reason, "result": result}})
}

func safeOwnerID(value string) bool {
	if value == "" || value == "core" || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func dropDataConfirmation(ownerID string) string {
	return "DROP_DATA_" + strings.ToUpper(strings.ReplaceAll(ownerID, "-", "_"))
}
