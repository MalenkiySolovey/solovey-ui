// Package dbtransfer owns database export and restore HTTP orchestration.
package dbtransfer

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	SettingService      service.SettingService
	NotifyEvent         func(string, map[string]string)
	RequireScope        func(*gin.Context, string, ...string) bool
	Audit               func(*gin.Context, string, string, string, string, map[string]any)
	Actor               func(*gin.Context) string
	RemoteIP            func(*gin.Context) string
	JSONMsg             func(*gin.Context, string, error)
	RequireStepUp       func(*gin.Context, string, string) bool
	EnforceStepUpHeader bool
	DataLifecycle       *datalifecycle.Manager
}

// Deps contains the host capabilities required by database transfer routes.
type Deps struct {
	SettingService      service.SettingService
	NotifyEvent         func(string, map[string]string)
	RequireScope        func(*gin.Context, string, ...string) bool
	Audit               func(*gin.Context, string, string, string, string, map[string]any)
	Actor               func(*gin.Context) string
	RemoteIP            func(*gin.Context) string
	JSONMsg             func(*gin.Context, string, error)
	RequireStepUp       func(*gin.Context, string, string) bool
	EnforceStepUpHeader bool
	DataLifecycle       *datalifecycle.Manager
}

func NewHandler(deps Deps) *Handler {
	notifyEvent := deps.NotifyEvent
	if notifyEvent == nil {
		notifyEvent = func(string, map[string]string) {}
	}
	return &Handler{
		SettingService:      deps.SettingService,
		NotifyEvent:         notifyEvent,
		RequireScope:        deps.RequireScope,
		Audit:               deps.Audit,
		Actor:               deps.Actor,
		RemoteIP:            deps.RemoteIP,
		JSONMsg:             deps.JSONMsg,
		RequireStepUp:       deps.RequireStepUp,
		EnforceStepUpHeader: deps.EnforceStepUpHeader,
		DataLifecycle:       deps.DataLifecycle,
	}
}

// RegisterRoutes mounts database import and export endpoints.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	g.POST("/importdb", h.ImportDb)
	g.GET("/getdb", h.DownloadDatabase)
	g.POST("/v1/operations/data/restore/rehearsal", h.RehearseDb)
	g.POST("/v1/operations/data/restore", h.ImportDb)
}
