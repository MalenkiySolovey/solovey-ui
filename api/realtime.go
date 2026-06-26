package api

import (
	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/gin-gonic/gin"
)

func (a *ApiService) realtimeHandler() *realtimehttp.Handler {
	return &realtimehttp.Handler{
		SettingService: a.SettingService,
		LoginUser:      GetLoginUser,
		RemoteIP:       getRemoteIp,
		Scope:          realtimeScopeFromContext,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	}
}

func realtimeScopeFromContext(c *gin.Context) realtime.Scope {
	switch c.GetString(apiTokenScopeKey) {
	case "":
		return realtime.ScopeAdmin
	case string(realtime.ScopeAdmin):
		return realtime.ScopeAdmin
	case string(realtime.ScopeRead):
		return realtime.ScopeRead
	case string(realtime.ScopeWrite):
		return realtime.ScopeWrite
	case string(realtime.ScopeObservability):
		return realtime.ScopeObservability
	default:
		return realtime.ScopeRead
	}
}
