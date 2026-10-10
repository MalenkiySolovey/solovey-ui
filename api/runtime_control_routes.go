package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid/v5"
)

type runtimeCapabilities struct {
	Compiled    bool `json:"compiled"`
	FlowClose   bool `json:"flowClose"`
	ParentClose bool `json:"parentClose"`
}

type runtimeSessionsView struct {
	Status               coreruntime.RuntimeStatus       `json:"status"`
	Snapshot             *coreruntime.ConnectionSnapshot `json:"snapshot,omitempty"`
	Capabilities         runtimeCapabilities             `json:"capabilities"`
	Maintenance          bool                            `json:"maintenance"`
	MaintenanceAvailable bool                            `json:"maintenanceAvailable"`
	Reason               string                          `json:"reason,omitempty"`
}

func (a *ApiService) registerRuntimeRoutes(g *gin.RouterGroup) {
	g.GET("/runtime/status", a.getRuntimeStatus)
	g.GET("/runtime/sessions", a.getRuntimeSessions)
	g.POST("/runtime/disconnect", a.disconnectRuntimeSessions)
	g.GET("/runtime/groups", a.getRuntimeGroups)
	g.POST("/runtime/select", a.selectRuntimeGroup)
	g.POST("/runtime/probe", a.probeRuntimeGroup)
	g.POST("/runtime/maintenance", a.setRuntimeMaintenance)
	g.GET("/runtime/logs", a.streamRuntimeLogs)
}

func runtimeReason(err error) string {
	switch {
	case errors.Is(err, coreruntime.ErrStaleGeneration):
		return "stale_generation"
	case errors.Is(err, coreruntime.ErrStaleInboundEpoch):
		return "stale_inbound_epoch"
	case errors.Is(err, coreruntime.ErrQUICParentIdentity):
		return "parent_identity_unavailable"
	case errors.Is(err, coreruntime.ErrCoreUnavailable):
		return "core_unavailable"
	case errors.Is(err, coreruntime.ErrRuntimeLimit):
		return "runtime_limit_exceeded"
	case errors.Is(err, coreruntime.ErrRuntimeAPIUnavailable):
		return "runtime_api_unavailable"
	case errors.Is(err, service.ErrMaintenanceUnavailable):
		return "maintenance_unavailable"
	default:
		return "runtime_error"
	}
}

func (a *ApiService) runtimeCore() *coreruntime.Core {
	if a.Runtime == nil {
		return service.DefaultRuntime().Core()
	}
	return a.Runtime.Core()
}

func runtimeWriteAllowed(c *gin.Context) bool {
	scope, token := requestTokenScope(c)
	return !token || scope == "admin" || scope == "write"
}

func (a *ApiService) runtimeView(c *gin.Context) runtimeSessionsView {
	view := runtimeSessionsView{
		Status:       a.runtimeCore().RuntimeStatus(c.Request.Context()),
		Capabilities: runtimeCapabilities{Compiled: registry.PrivateRuntimeAPICompiled(), FlowClose: runtimeWriteAllowed(c), ParentClose: runtimeWriteAllowed(c) && registry.PrivateRuntimeAPICompiled() && registry.QUICParentControlCompiled()},
	}
	held, err := a.SettingService.CoreMaintenance()
	view.Maintenance, view.MaintenanceAvailable = held, err == nil
	if err != nil {
		view.Reason = "maintenance_unavailable"
		view.Capabilities.FlowClose = false
		view.Capabilities.ParentClose = false
	}
	return view
}

func (a *ApiService) getRuntimeStatus(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_status", "admin", "read", "write", "observability") {
		return
	}
	c.JSON(http.StatusOK, Msg{Success: true, Obj: a.runtimeView(c)})
}

func (a *ApiService) getRuntimeSessions(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_sessions", "admin", "read", "write") {
		return
	}
	clientID, err := strconv.ParseUint(c.DefaultQuery("clientId", "0"), 10, strconv.IntSize)
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "100"))
	generation := c.Query("generation")
	if err != nil || limitErr != nil || limit < 1 || limit > 256 || len(generation) > 64 {
		c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
		return
	}
	view := a.runtimeView(c)
	if !view.MaintenanceAvailable {
		c.JSON(http.StatusOK, Msg{Success: true, Obj: view})
		return
	}
	if !view.Status.APIAvailable {
		view.Reason = view.Status.Reason
		if view.Reason == "" {
			view.Reason = "core_unavailable"
		}
		c.JSON(http.StatusOK, Msg{Success: true, Obj: view})
		return
	}
	if generation == "" {
		generation = view.Status.Generation
	}
	snapshot, err := a.runtimeCore().Connections(c.Request.Context(), generation, uint(clientID), limit)
	if err != nil {
		view.Reason = runtimeReason(err)
	} else {
		view.Snapshot = &snapshot
	}
	c.JSON(http.StatusOK, Msg{Success: true, Obj: view})
}

func (a *ApiService) disconnectRuntimeSessions(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_disconnect", "admin", "write") {
		return
	}
	var request coreruntime.DisconnectRequest
	decoded := decodeRuntimeDisconnect(c, &request)
	_, generationErr := uuid.FromString(request.Generation)
	var flowErr error
	if request.FlowID != "" {
		_, flowErr = uuid.FromString(request.FlowID)
	}
	parentsValid := len(request.Parents) <= 128 && (len(request.Parents) == 0 || request.FlowID == "" && request.ClientID != 0)
	for _, parent := range request.Parents {
		id, err := strconv.ParseUint(parent.ParentID, 10, 64)
		parentsValid = parentsValid && err == nil && id != 0 && strconv.FormatUint(id, 10) == parent.ParentID && validRuntimeTag(parent.Inbound) && validRuntimeGeneration(parent.Epoch)
	}
	if !decoded || generationErr != nil || flowErr != nil || !parentsValid || request.FlowID == "" && request.ClientID == 0 {
		c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()
	result := coreruntime.DisconnectResult{Generation: request.Generation, Outcome: "ERROR", ParentControl: "not_supported"}
	var err error
	if core := a.runtimeCore(); core != nil {
		result, err = core.Disconnect(ctx, request)
	} else {
		err = coreruntime.ErrCoreUnavailable
	}
	if err != nil {
		result.Reason = runtimeReason(err)
		if errors.Is(err, coreruntime.ErrStaleGeneration) {
			result.Outcome = "STALE_GENERATION"
		}
		if errors.Is(err, coreruntime.ErrStaleInboundEpoch) {
			result.Outcome = "STALE_INBOUND_EPOCH"
		}
	}
	a.recordAuditSynchronous(c, requestActor(c), "runtime_disconnect", "runtime", service.AuditSeverityInfo, map[string]any{
		"generation": request.Generation, "client_id": request.ClientID, "flow_id": request.FlowID,
		"outcome": result.Outcome, "matched": result.Matched, "closed": result.Closed,
		"remaining": result.Remaining, "parent_closed": result.ParentClosed, "parents_matched": result.ParentsMatched,
		"parents_closed": result.ParentsClosed, "parents_remaining": result.ParentsRemaining, "reason": result.Reason,
	})
	c.JSON(http.StatusOK, Msg{Success: err == nil, Obj: result})
}

// Up to 128 explicit parent targets need a bounded larger envelope. Preserve
// the established 2 KiB limit for every flow-only request and strict decoding.
func decodeRuntimeDisconnect(c *gin.Context, target *coreruntime.DisconnectRequest) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	return len(target.Parents) != 0 || len(body) <= 2048
}
