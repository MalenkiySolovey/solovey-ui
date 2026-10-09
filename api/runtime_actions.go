package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	apiconfig "github.com/MalenkiySolovey/solovey-ui/api/config"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid/v5"
)

func decodeRuntimeRequest(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func validRuntimeGeneration(value string) bool {
	_, err := uuid.FromString(value)
	return err == nil
}

func validRuntimeTag(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func (a *ApiService) getRuntimeGroups(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_groups", "admin", "read", "write") {
		return
	}
	view := a.runtimeView(c)
	var snapshot *coreruntime.GroupSnapshot
	if view.MaintenanceAvailable && !view.Maintenance && view.Status.APIAvailable {
		generation := c.Query("generation")
		if generation == "" {
			generation = view.Status.Generation
		}
		if !validRuntimeGeneration(generation) {
			c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
			return
		}
		groups, err := a.runtimeCore().Groups(c.Request.Context(), generation)
		if err != nil {
			view.Reason = runtimeReason(err)
		} else {
			snapshot = &groups
		}
	}
	c.JSON(http.StatusOK, Msg{Success: true, Obj: gin.H{"runtime": view, "snapshot": snapshot, "canWrite": runtimeWriteAllowed(c), "canMaintenance": runtimeMaintenanceAllowed(c)}})
}

type runtimeGroupRequest struct {
	Generation string `json:"generation"`
	Group      string `json:"group"`
	Member     string `json:"member"`
	Target     string `json:"target,omitempty"`
}

func (a *ApiService) selectRuntimeGroup(c *gin.Context) { a.runtimeGroupAction(c, false) }
func (a *ApiService) probeRuntimeGroup(c *gin.Context)  { a.runtimeGroupAction(c, true) }

func (a *ApiService) runtimeGroupAction(c *gin.Context, probe bool) {
	event := "runtime_select"
	if probe {
		event = "runtime_probe"
	}
	if !a.requireTokenScopeAny(c, event, "admin", "write") {
		return
	}
	var request runtimeGroupRequest
	if !decodeRuntimeRequest(c, &request) || !validRuntimeGeneration(request.Generation) || !validRuntimeTag(request.Group) || !validRuntimeTag(request.Member) || !probe && request.Target != "" {
		c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 16*time.Second)
	defer cancel()
	if probe && request.Target != "" {
		if len(request.Target) > 1024 || apiconfig.ValidateOutboundCheckTarget(ctx, request.Target) != nil {
			c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_probe_target"})
			return
		}
	}
	result := gin.H{"generation": request.Generation, "outcome": "ERROR"}
	var err error
	if core := a.runtimeCore(); core == nil {
		err = coreruntime.ErrCoreUnavailable
	} else if probe {
		check := core.CheckRuntimeGroupOutbound(ctx, request.Generation, request.Group, request.Member, request.Target)
		result["ok"], result["delay"], result["reason"] = check.OK, check.Delay, check.Error
		if check.OK {
			result["outcome"] = "PROBE_COMPLETED"
		}
	} else {
		err = core.SelectRuntimeGroup(ctx, request.Generation, request.Group, request.Member)
		if err == nil {
			result["outcome"] = "RUNTIME_SELECTED"
		}
	}
	if err != nil {
		result["reason"] = runtimeReason(err)
	}
	// An arbitrary request string must not become audit plaintext. The digest
	// permits target correlation without recording caller-supplied credentials.
	digest := sha256.Sum256([]byte(request.Group + "\x00" + request.Member))
	a.recordAuditSynchronous(c, requestActor(c), event, "runtime", service.AuditSeverityInfo, map[string]any{"generation": request.Generation, "target_sha256": hex.EncodeToString(digest[:]), "outcome": result["outcome"], "reason": result["reason"]})
	c.JSON(http.StatusOK, Msg{Success: err == nil, Obj: result})
}

// Maintenance controls all clients' deliberate downtime. Existing admin scope
// grants lifecycle authority; the narrower write scope grants flow/group actions.
func runtimeMaintenanceAllowed(c *gin.Context) bool {
	scope, token := requestTokenScope(c)
	return !token || scope == "admin"
}

func (a *ApiService) setRuntimeMaintenance(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "runtime_maintenance", "admin") {
		return
	}
	var request struct {
		Generation string `json:"generation"`
		Enabled    *bool  `json:"enabled"`
	}
	if !decodeRuntimeRequest(c, &request) || request.Enabled == nil || request.Generation != "" && !validRuntimeGeneration(request.Generation) {
		c.JSON(http.StatusBadRequest, Msg{Msg: "invalid_runtime_request"})
		return
	}
	config := a.ConfigService
	if config == nil {
		config = service.NewConfigServiceWithRuntime(a.Runtime)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	err := config.SetCoreMaintenance(ctx, request.Generation, *request.Enabled)
	view := a.runtimeView(c)
	if err != nil {
		view.Reason = runtimeReason(err)
	}
	a.recordAuditSynchronous(c, requestActor(c), "runtime_maintenance", "runtime", service.AuditSeverityInfo, map[string]any{"generation": request.Generation, "desired_hold": *request.Enabled, "actual_state": view.Status.State, "applied": err == nil, "reason": view.Reason})
	c.JSON(http.StatusOK, Msg{Success: err == nil, Obj: view})
}
