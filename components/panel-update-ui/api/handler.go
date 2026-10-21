package api

import (
	requestcontext "context"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"net/http"

	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type ComponentCatalog interface {
	Inventory() (panelupdateservice.Inventory, error)
}

type ComponentManager interface {
	Enable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Disable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Install(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Remove(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
}

type Deps struct {
	Components       ComponentCatalog
	ComponentManager ComponentManager

	LoginUser     func(*gin.Context) string
	RemoteIP      func(*gin.Context) string
	Hostname      func(*gin.Context) string
	RequireScope  func(*gin.Context, string, ...string) bool
	RequireStepUp func(*gin.Context, string, string) bool
	ActionPolicy  func(*gin.Context) actions.Authorizer
	Audit         func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj       func(*gin.Context, any, error)
	JSONMsg       func(*gin.Context, string, error)
}

type Handler struct{ deps Deps }

func RegisterRoutes(group *gin.RouterGroup, deps Deps) {
	handler := Handler{deps: deps}
	routes := group.Group("/update")
	if deps.Components != nil {
		routes.GET("/components", handler.components)
	}
	if deps.ComponentManager != nil {
		routes.POST("/components/:id/enable", handler.componentEnable)
		routes.POST("/components/:id/disable", handler.componentDisable)
		routes.POST("/components/:id/install", handler.componentInstall)
		routes.POST("/components/:id/remove", handler.componentRemove)
	}
}

func (h Handler) components(context *gin.Context) {
	if !h.requireComponentUpdateScope(context) || !h.requireComponentDependencies(context, false) {
		return
	}
	inventory, err := h.deps.Components.Inventory()
	h.deps.JSONObj(context, inventory, err)
}

func (h Handler) componentEnable(context *gin.Context) {
	h.componentSetEnabled(context, true)
}

func (h Handler) componentDisable(context *gin.Context) {
	h.componentSetEnabled(context, false)
}

func (h Handler) componentSetEnabled(context *gin.Context, enabled bool) {
	if !h.requireComponentUpdateScope(context) || !h.requireComponentDependencies(context, true) {
		return
	}
	id, ok := h.validComponentID(context)
	if !ok {
		return
	}
	var (
		status panelupdateservice.ComponentStatus
		err    error
	)
	if enabled {
		status, err = h.deps.ComponentManager.Enable(h.componentOperationContext(context), id)
	} else {
		status, err = h.deps.ComponentManager.Disable(h.componentOperationContext(context), id)
	}
	if err != nil {
		context.AbortWithStatusJSON(http.StatusConflict, gin.H{"success": false, "msg": "component state change rejected"})
		return
	}
	h.deps.Audit(context, h.deps.LoginUser(context), "component_enabled_changed", "component", service.AuditSeverityInfo, map[string]any{
		"component": id,
		"enabled":   enabled,
	})
	h.deps.JSONObj(context, status, nil)
}

func (h Handler) componentInstall(context *gin.Context) {
	if !h.requireComponentUpdateScope(context) || !h.requireComponentDependencies(context, true) {
		return
	}
	id, ok := h.validComponentID(context)
	if !ok {
		return
	}
	status, err := h.deps.ComponentManager.Install(h.componentOperationContext(context), id)
	if err != nil {
		h.deps.JSONMsg(context, "", err)
		return
	}
	h.deps.Audit(context, h.deps.LoginUser(context), "component_installed_changed", "component", service.AuditSeverityWarn, map[string]any{
		"component": id,
		"installed": true,
	})
	h.deps.JSONObj(context, status, nil)
}

func (h Handler) componentRemove(context *gin.Context) {
	if !h.requireComponentUpdateScope(context) || !h.requireComponentDependencies(context, true) {
		return
	}
	id, ok := h.validComponentID(context)
	if !ok {
		return
	}
	if h.deps.RequireStepUp == nil || h.deps.ActionPolicy == nil {
		context.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "component step-up boundary unavailable"})
		return
	}
	var actionInvocation actions.Invocation
	policy := h.deps.ActionPolicy(context)
	operation := h.componentOperationContext(context)
	operation.Authorize = func(ctx requestcontext.Context, invocation actions.Invocation) (actions.Decision, error) {
		actionInvocation = invocation
		if policy == nil {
			return actions.Decision{}, actions.ErrDenied
		}
		return policy(ctx, invocation)
	}
	status, err := h.deps.ComponentManager.Remove(operation, id)
	if err != nil {
		if context.IsAborted() {
			return
		}
		h.deps.JSONMsg(context, "", err)
		return
	}
	h.deps.Audit(context, h.deps.LoginUser(context), "component_installed_changed", "component", service.AuditSeverityWarn, map[string]any{
		"actionId": actionInvocation.ID, "inputDigest": actionInvocation.InputDigest,
		"correlation": actionInvocation.Reference.Correlation(),
		"component":   id,
		"installed":   false,
	})
	h.deps.JSONObj(context, status, nil)
}

func (h Handler) requireComponentUpdateScope(context *gin.Context) bool {
	if h.deps.RequireScope == nil {
		context.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "component authorization boundary unavailable"})
		return false
	}
	return h.deps.RequireScope(context, "update", "admin", "update")
}

func (h Handler) requireComponentDependencies(context *gin.Context, mutation bool) bool {
	available := h.deps.Components != nil && h.deps.JSONObj != nil
	if mutation {
		available = h.deps.ComponentManager != nil && h.deps.LoginUser != nil && h.deps.RemoteIP != nil &&
			h.deps.Audit != nil && h.deps.JSONObj != nil && h.deps.JSONMsg != nil
	}
	if available {
		return true
	}
	context.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "component management is unavailable"})
	return false
}

func (h Handler) validComponentID(context *gin.Context) (string, bool) {
	id := context.Param("id")
	if err := manifest.ValidateID(id); err != nil {
		h.deps.JSONMsg(context, "", err)
		return "", false
	}
	return id, true
}

func (h Handler) componentOperationContext(context *gin.Context) panelupdateservice.OperationContext {
	actor := ""
	if h.deps.LoginUser != nil {
		actor = h.deps.LoginUser(context)
	}
	hostname := ""
	if h.deps.Hostname != nil {
		hostname = h.deps.Hostname(context)
	}
	return panelupdateservice.OperationContext{
		RequestContext: context.Request.Context(),
		Actor:          actor,
		Hostname:       hostname,
	}
}
