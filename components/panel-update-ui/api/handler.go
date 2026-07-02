package api

import (
	"net/http"

	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configupdate "github.com/MalenkiySolovey/solovey-ui/config/update"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/gin-gonic/gin"
)

type Settings interface {
	GetUpdateChannel() string
	SetUpdateChannel(string) error
}

type Versions interface {
	CheckForChannel(string, bool) serviceupdate.VersionInfo
	ResolveTarget(string) (serviceupdate.ReleaseTarget, error)
}

type Manager interface {
	Status() serviceupdate.UpdateJob
	Apply(serviceupdate.ReleaseTarget, string) error
}

type ComponentCatalog interface {
	Inventory() (panelupdateservice.Inventory, error)
}

type ComponentManager interface {
	Enable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Disable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Install(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error)
	Remove(panelupdateservice.OperationContext, string, bool) (panelupdateservice.ComponentStatus, error)
}

type Deps struct {
	Settings         Settings
	Versions         Versions
	Manager          Manager
	Components       ComponentCatalog
	ComponentManager ComponentManager

	LoginUser      func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	Hostname       func(*gin.Context) string
	RequireScope   func(*gin.Context, string, ...string) bool
	CheckPassword  func(string, string, string) bool
	CheckRateLimit func(string) error
	RecordFailure  func(string)
	ResetFailures  func(string)
	UserKey        func(string) string
	AllowCheck     func() bool
	Audit          func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj        func(*gin.Context, any, error)
	JSONMsg        func(*gin.Context, string, error)
}

type Handler struct{ deps Deps }

type StatusResponse struct {
	serviceupdate.VersionInfo
	Job serviceupdate.UpdateJob `json:"job"`
}

func RegisterRoutes(group *gin.RouterGroup, deps Deps) {
	handler := Handler{deps: deps}
	routes := group.Group("/update")
	routes.GET("/status", handler.status)
	routes.POST("/check", handler.check)
	routes.POST("/apply", handler.apply)
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

func (h Handler) response(info serviceupdate.VersionInfo) StatusResponse {
	return StatusResponse{VersionInfo: info, Job: h.deps.Manager.Status()}
}

func (h Handler) status(context *gin.Context) {
	channel := h.deps.Settings.GetUpdateChannel()
	h.deps.JSONObj(context, h.response(h.deps.Versions.CheckForChannel(channel, false)), nil)
}

func (h Handler) check(context *gin.Context) {
	channel := configupdate.NormalizeChannel(context.DefaultPostForm("channel", h.deps.Settings.GetUpdateChannel()))
	if err := h.deps.Settings.SetUpdateChannel(channel); err != nil {
		h.deps.JSONMsg(context, "update", err)
		return
	}
	force := h.deps.AllowCheck()
	info := h.deps.Versions.CheckForChannel(channel, force)
	h.deps.Audit(context, h.deps.LoginUser(context), "panel_update_check", "update", "info", map[string]any{
		"channel": channel, "latest": info.Latest, "rateLimited": !force,
	})
	h.deps.JSONObj(context, h.response(info), nil)
}

func (h Handler) apply(context *gin.Context) {
	user := h.deps.LoginUser(context)
	remoteIP := h.deps.RemoteIP(context)
	channel := configupdate.NormalizeChannel(context.DefaultPostForm("channel", h.deps.Settings.GetUpdateChannel()))
	password := context.PostForm("password")
	targetVersion := context.PostForm("targetVersion")
	if password == "" {
		h.auditApply(context, user, channel, "", "reauth_required")
		h.deps.JSONMsg(context, "update", common.NewError("re-authentication required"))
		return
	}
	userKey := h.deps.UserKey(user)
	if err := h.deps.CheckRateLimit(remoteIP); err != nil {
		h.auditApply(context, user, channel, "", "rate_limited")
		h.deps.JSONMsg(context, "update", err)
		return
	}
	if err := h.deps.CheckRateLimit(userKey); err != nil {
		h.auditApply(context, user, channel, "", "rate_limited")
		h.deps.JSONMsg(context, "update", err)
		return
	}
	if !h.deps.CheckPassword(user, password, remoteIP) {
		h.deps.RecordFailure(remoteIP)
		h.deps.RecordFailure(userKey)
		h.auditApply(context, user, channel, "", "reauth_failed")
		h.deps.JSONMsg(context, "update", common.NewError("re-authentication failed"))
		return
	}
	h.deps.ResetFailures(remoteIP)
	h.deps.ResetFailures(userKey)

	target, err := h.deps.Versions.ResolveTarget(channel)
	if err != nil {
		h.auditApply(context, user, channel, "", "no_target")
		h.deps.JSONMsg(context, "update", err)
		return
	}
	if !targetVersionMatches(targetVersion, target) {
		h.auditApply(context, user, channel, target.Version, "version_changed")
		h.deps.JSONMsg(context, "update", common.NewError("available version changed; re-run Check updates"))
		return
	}
	if err := h.deps.Manager.Apply(target, user); err != nil {
		h.auditApply(context, user, channel, target.Version, "rejected")
		h.deps.JSONMsg(context, "update", err)
		return
	}
	h.auditApply(context, user, channel, target.Version, "started")
	h.deps.JSONObj(context, h.response(h.deps.Versions.CheckForChannel(channel, false)), nil)
}

func (h Handler) auditApply(context *gin.Context, user, channel, target, result string) {
	severity := "info"
	if result != "started" {
		severity = "warn"
	}
	h.deps.Audit(context, user, "panel_update_apply", "update", severity, map[string]any{
		"channel": channel, "from": configidentity.GetVersion(), "to": target, "result": result,
	})
}

func (h Handler) components(context *gin.Context) {
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
	if !h.requireComponentUpdateScope(context) {
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
		context.AbortWithStatusJSON(http.StatusConflict, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.deps.Audit(context, h.deps.LoginUser(context), "component_enabled_changed", "component", service.AuditSeverityInfo, map[string]any{
		"component": id,
		"enabled":   enabled,
	})
	h.deps.JSONObj(context, status, nil)
}

func (h Handler) componentInstall(context *gin.Context) {
	if !h.requireComponentUpdateScope(context) {
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

type componentRemoveRequest struct {
	Password   string `json:"password" form:"password"`
	DeleteData bool   `json:"deleteData" form:"deleteData"`
}

func (h Handler) componentRemove(context *gin.Context) {
	if !h.requireComponentUpdateScope(context) {
		return
	}
	id, ok := h.validComponentID(context)
	if !ok {
		return
	}
	var request componentRemoveRequest
	if err := context.ShouldBind(&request); err != nil {
		h.deps.JSONMsg(context, "", err)
		return
	}
	user := h.deps.LoginUser(context)
	remoteIP := h.deps.RemoteIP(context)
	if user == "" || request.Password == "" {
		h.deps.JSONMsg(context, "", common.NewError("re-authentication required"))
		return
	}
	if !h.deps.CheckPassword(user, request.Password, remoteIP) {
		h.deps.Audit(context, user, "component_remove_reauth_failed", "component", service.AuditSeverityWarn, map[string]any{"component": id})
		h.deps.JSONMsg(context, "", common.NewError("re-authentication failed"))
		return
	}
	status, err := h.deps.ComponentManager.Remove(h.componentOperationContext(context), id, request.DeleteData)
	if err != nil {
		h.deps.JSONMsg(context, "", err)
		return
	}
	h.deps.Audit(context, user, "component_installed_changed", "component", service.AuditSeverityWarn, map[string]any{
		"component":  id,
		"installed":  false,
		"deleteData": request.DeleteData,
	})
	h.deps.JSONObj(context, status, nil)
}

func (h Handler) requireComponentUpdateScope(context *gin.Context) bool {
	if h.deps.RequireScope == nil {
		return true
	}
	return h.deps.RequireScope(context, "update", "admin", "update")
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
	hostname := ""
	if h.deps.Hostname != nil {
		hostname = h.deps.Hostname(context)
	}
	return panelupdateservice.OperationContext{
		Actor:    h.deps.LoginUser(context),
		Hostname: hostname,
	}
}

func targetVersionMatches(requested string, target serviceupdate.ReleaseTarget) bool {
	if requested == "" {
		return false
	}
	if requested == target.Tag || requested == target.Version {
		return true
	}
	return versionpolicy.NormalizeVersion(requested) == target.Version
}
