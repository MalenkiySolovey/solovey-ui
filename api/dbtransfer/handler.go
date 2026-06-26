// Package dbtransfer owns database export and restore HTTP orchestration.
package dbtransfer

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	SettingService  service.SettingService
	TelegramService service.TelegramService
	RequireScope    func(*gin.Context, string, ...string) bool
	Audit           func(*gin.Context, string, string, string, string, map[string]any)
	Actor           func(*gin.Context) string
	RemoteIP        func(*gin.Context) string
	JSONMsg         func(*gin.Context, string, error)
}

// Deps contains the host capabilities required by database transfer routes.
type Deps struct {
	SettingService  service.SettingService
	TelegramService service.TelegramService
	RequireScope    func(*gin.Context, string, ...string) bool
	Audit           func(*gin.Context, string, string, string, string, map[string]any)
	Actor           func(*gin.Context) string
	RemoteIP        func(*gin.Context) string
	JSONMsg         func(*gin.Context, string, error)
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		SettingService:  deps.SettingService,
		TelegramService: deps.TelegramService,
		RequireScope:    deps.RequireScope,
		Audit:           deps.Audit,
		Actor:           deps.Actor,
		RemoteIP:        deps.RemoteIP,
		JSONMsg:         deps.JSONMsg,
	}
}

// RegisterRoutes mounts database import and export endpoints.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	g.POST("/importdb", h.ImportDb)
	g.GET("/getdb", h.DownloadDatabase)
}
