package api

import (
	"sync"

	authhttp "github.com/MalenkiySolovey/solovey-ui/api/auth"
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	failoverhttp "github.com/MalenkiySolovey/solovey-ui/api/failover"
	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	ApiService
	apiv2           *APIv2Handler
	csrfLoginPath   string
	authExemptPaths map[string]struct{}
	proxyConfigMu   sync.Mutex
	proxyConfigSeen bool
	proxyRevision   string
}

func NewAPIHandler(g *gin.RouterGroup, a2 *APIv2Handler, options ...Option) {
	a := &APIHandler{
		ApiService: NewApiService(options...),
		apiv2:      a2,
	}
	a.initRouter(g)
}

func (a *APIHandler) initRouter(g *gin.RouterGroup) {
	a.csrfLoginPath = a.cachedCSRFLoginPath()
	a.authExemptPaths = a.cachedAuthExemptPaths()
	g.Use(a.requestAuthorityMiddleware)
	g.Use(func(c *gin.Context) {
		if _, exempt := a.authExemptPaths[c.Request.URL.Path]; !exempt {
			checkLogin(c)
		}
	})
	g.Use(a.csrfMiddleware)
	g.Use(restrictPreauthSession)
	a.registerGroupedRoutes(g)
}

func (a *APIHandler) cachedAuthExemptPaths() map[string]struct{} {
	webPath, err := a.SettingService.GetWebPath()
	if err != nil {
		webPath = "/"
	}
	return map[string]struct{}{
		joinURL(webPath, "api/login"):  {},
		joinURL(webPath, "api/logout"): {},
		joinURL(webPath, "api/csrf"):   {},
	}
}

func (a *APIHandler) registerGroupedRoutes(g *gin.RouterGroup) {
	authDeps := a.authDeps()
	authDeps.CSRF = a.ApiService.GetCSRF
	authDeps.ReloadTokensAfter = a.reloadTokensAfter
	authDeps.RequireStepUp = a.requireStepUpAction
	authhttp.RegisterRoutes(g, authDeps)
	a.registerSecurityRoutes(g)
	a.registerSSHManagementRoutes(g)
	a.registerDeploymentRoutes(g)
	a.registerUpdateRoutes(g)
	a.registerOperationsStatusRoutes(g)
	a.registerDataLifecycleRoutes(g)

	configDeps := a.configDeps()
	configDeps.LoginUser = GetLoginUser
	confighttp.RegisterRoutes(g, configDeps)

	dbTransferDeps := a.dbTransferDeps()
	dbTransferDeps.RequireStepUp = a.requireStepUpAction
	dbTransferDeps.EnforceStepUpHeader = true
	dbtransferhttp.RegisterRoutes(g, dbTransferDeps)
	telemetryhttp.RegisterCoreRoutes(g, a.telemetryDeps())
	failoverhttp.RegisterRoutes(g, failoverhttp.Deps{
		Status:  service.FailoverStatusEntries,
		JSONObj: jsonObj,
	})

	a.registerComponentStatusRoutes(g)
	registerComponentAPIRoutes(g, a.componentAPI(a.requireStepUpAction))

	realtimehttp.RegisterRoutes(g, realtimehttp.Deps{
		SettingService: a.SettingService,
		LoginUser:      GetLoginUser,
		RemoteIP:       getRemoteIp,
		SessionBinding: realtimeSessionBinding,
		SessionValid:   realtimeSessionValid,
		Scope:          realtimeScopeFromContext,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	})

}

func (a *APIHandler) reloadTokensAfter(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler(c)
		if a.apiv2 != nil {
			a.apiv2.ReloadTokens()
		}
	}
}
