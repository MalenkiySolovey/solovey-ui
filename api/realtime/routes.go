package realtimehttp

import (
	apprealtime "github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

// Deps contains the host capabilities required by the realtime transport.
type Deps struct {
	SettingService service.SettingService
	LoginUser      func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
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
		Scope:          deps.Scope,
		Audit:          deps.Audit,
		JSONObj:        deps.JSONObj,
		JSONMsg:        deps.JSONMsg,
	}
	realtime := g.Group("/realtime")
	realtime.GET("/ws-token", h.IssueWSToken)
	realtime.GET("/ws", h.RealtimeWS)
}
