package api

import (
	"net/http"

	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) componentAPI(requireStepUp func(*gin.Context, string, string) bool) componenthost.APIDeps {
	return componenthost.APIDeps{
		Runtime: a.Runtime,
		Auth: componenthost.AuthDeps{
			RequireScope:           a.requireTokenScopeAny,
			RequireAuditAdminScope: a.requireAuditAdminScope,
			RequireStepUp:          requireStepUp,
			LoginUser:              GetLoginUser,
			CheckPassword: func(user, password, remoteIP string) bool {
				found, _ := a.UserService.CheckUser(user, password, remoteIP)
				return found != nil
			},
		},
		Request: componenthost.RequestDeps{
			Actor:          requestActor,
			RemoteIP:       getRemoteIp,
			Hostname:       getHostname,
			ValidateTarget: confighttp.ValidateOutboundCheckTarget,
		},
		Rate: componenthost.RateLimitDeps{
			CheckRateLimit:        checkComponentManualActionRateLimit,
			CheckLoginRateLimit:   checkLoginRateLimit,
			RecordLoginFailure:    recordLoginFailure,
			ResetLoginFailures:    resetLoginFailures,
			LoginRateLimitUserKey: loginRateLimitUserKey,
			CheckAuditRateLimit:   checkAuditEndpointRateLimit,
			AuditRateLimitKey:     auditEndpointRateLimitKey,
			AuditRateLimitWindow:  auditEndpointRateLimitWindow,
		},
		Audit: componenthost.AuditDeps{
			Audit: a.recordAudit,
		},
		HTTP: componenthost.HTTPDeps{
			JSONObj: jsonObj,
			JSONMsg: jsonMsg,
		},
		Update: componenthost.UpdateDeps{
			AllowForcedUpdateCheck: allowForcedUpdateCheck,
		},
	}
}

func registerComponentAPIRoutes(g *gin.RouterGroup, api componenthost.APIDeps) {
	state.InvalidateActiveCache()
	installed, err := state.InstalledIDs()
	if err != nil {
		logger.Warning("load installed component metadata err: ", err)
		return
	}
	for _, routes := range registry.APIRouteRegistrarsByComponentIDs(installed) {
		componentGroup := g.Group("", componentActiveMiddleware(routes.ComponentID))
		if err := routes.Register(componentGroup, api); err != nil {
			logger.Warning("register component API routes err: ", routes.ComponentID, ": ", err)
		}
	}
}

func componentActiveMiddleware(componentID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		active, err := state.IsActiveCached(componentID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, Msg{
				Success: false,
				Msg:     "component state unavailable",
			})
			return
		}
		if !active {
			c.AbortWithStatusJSON(http.StatusConflict, Msg{
				Success: false,
				Msg:     "component disabled: " + componentID,
			})
			return
		}
		c.Next()
	}
}

func (a *APIHandler) registerComponentStatusRoutes(g *gin.RouterGroup) {
	g.GET("/components", func(c *gin.Context) {
		components, err := state.Components()
		inventory := componentRuntimeInventory{
			BinaryProfile: componentprofile.Binary,
			Components:    components,
		}
		jsonObj(c, inventory, err)
	})
}

type componentRuntimeInventory struct {
	BinaryProfile string            `json:"binaryProfile"`
	Components    []state.Component `json:"components"`
}
