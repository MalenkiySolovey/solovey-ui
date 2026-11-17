package api

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-backend/componentroutes"
	"github.com/MalenkiySolovey/solovey-backend/componentview"
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) componentAPI(requireStepUp func(*gin.Context, string, string) bool, authorizationChanged func()) componentroutes.Ports {
	runtime := a.Runtime
	return componentroutes.Ports{
		ComponentSettings: a.ConfigService,
		Auth: componentroutes.AuthDeps{
			ActionPolicy:           a.actionPolicy(requireStepUp),
			AuthorizationChanged:   authorizationChanged,
			RequireScope:           a.requireTokenScopeAny,
			RequireAuditAdminScope: a.requireAuditAdminScope,
			RequireStepUp:          requireStepUp,
			LoginUser:              GetLoginUser,
			CheckPassword: func(user, password, remoteIP string) bool {
				found, _ := a.UserService.CheckUser(user, password, remoteIP)
				return found != nil
			},
		},
		Request: componentroutes.RequestDeps{
			Actor:          requestActor,
			RemoteIP:       getRemoteIp,
			ClientIdentity: RequestClientIdentity,
			Hostname:       getHostname,
			ValidateTarget: confighttp.ValidateOutboundCheckTarget,
		},
		Rate: componentroutes.RateLimitDeps{
			CheckRateLimit:        checkComponentManualActionRateLimit,
			CheckLoginRateLimit:   checkLoginRateLimit,
			RecordLoginFailure:    recordLoginFailure,
			ResetLoginFailures:    resetLoginFailures,
			LoginRateLimitUserKey: loginRateLimitUserKey,
			CheckAuditRateLimit:   checkAuditEndpointRateLimit,
			AuditRateLimitKey:     auditEndpointRateLimitKey,
			AuditRateLimitWindow:  auditEndpointRateLimitWindow,
		},
		Audit: componentroutes.AuditDeps{
			Audit:    a.recordAudit,
			Recorder: &service.AuditService{Runtime: runtime},
		},
		HTTP: componentroutes.HTTPDeps{
			JSONObj: jsonObj,
			JSONMsg: jsonMsg,
		},
		Update: componentroutes.UpdateDeps{
			AllowForcedUpdateCheck: allowForcedUpdateCheck,
		},
	}
}

func (a *ApiService) registerComponentAPIRoutes(g *gin.RouterGroup, ports componentroutes.Ports) {
	if a.Components == nil {
		logger.Warning("register component API routes err: ", componentroutes.ErrUnavailable)
		return
	}
	routes, err := a.Components.Routes(ports)
	if err != nil {
		logger.Warning("load installed component metadata err: ", err)
		return
	}
	for _, route := range routes {
		componentGroup := g.Group("", a.componentActiveMiddleware(route.ComponentID))
		if err := route.Register(componentGroup); err != nil {
			logger.Warning("register component API routes err: ", route.ComponentID, ": ", err)
		}
	}
}

const componentAuthorityKey = "componenthost.authority.id"

func (a *ApiService) componentActiveMiddleware(componentID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		active, err := false, componentroutes.ErrUnavailable
		if a.Components != nil {
			active, err = a.Components.IsActive(componentID)
		}
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
		c.Set(componentAuthorityKey, componentID)
		c.Next()
	}
}

func (a *APIHandler) registerComponentStatusRoutes(g *gin.RouterGroup) {
	g.GET("/components", func(c *gin.Context) {
		inventory, err := componentroutes.Inventory(a.Components)
		jsonObj(c, inventory, err)
	})
}

type componentRuntimeInventory = componentview.RuntimeInventory
