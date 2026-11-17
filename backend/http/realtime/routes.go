package realtimehttp

import (
	apprealtime "github.com/MalenkiySolovey/solovey-ui/backend/realtime"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"

	"github.com/gin-gonic/gin"
)

// Deps contains the host capabilities required by the realtime transport.
type Deps struct {
	SettingService Settings
	LoginUser      func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	SessionBinding func(*gin.Context) (panelsecurity.RealtimeSessionBinding, bool)
	SessionValid   func(panelsecurity.RealtimeSessionBinding) bool
	Scope          func(*gin.Context) apprealtime.Scope
	Audit          func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj        func(*gin.Context, interface{}, error)
	JSONMsg        func(*gin.Context, string, error)
}

// RegisterRoutes mounts realtime endpoints on an already secured API group.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := &Handler{
		SettingService: deps.SettingService,
		LoginUser:      deps.LoginUser,
		RemoteIP:       deps.RemoteIP,
		SessionBinding: deps.SessionBinding,
		SessionValid:   deps.SessionValid,
		Scope:          deps.Scope,
		Audit:          deps.Audit,
		JSONObj:        deps.JSONObj,
		JSONMsg:        deps.JSONMsg,
	}
	realtime := g.Group("/realtime")
	realtime.POST("/ws-token", h.IssueWSToken)
	realtime.GET("/ws", h.RealtimeWS)
}
