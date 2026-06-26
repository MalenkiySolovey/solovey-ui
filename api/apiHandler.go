package api

import (
	authhttp "github.com/MalenkiySolovey/solovey-ui/api/auth"
	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	failoverhttp "github.com/MalenkiySolovey/solovey-ui/api/failover"
	importxuihttp "github.com/MalenkiySolovey/solovey-ui/api/importxui"
	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	remotesubhttp "github.com/MalenkiySolovey/solovey-ui/api/remotesub"
	telegramhttp "github.com/MalenkiySolovey/solovey-ui/api/telegram"
	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	updatehttp "github.com/MalenkiySolovey/solovey-ui/api/update"
	paidadmin "github.com/MalenkiySolovey/solovey-ui/paidsub/admin"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	ApiService
	apiv2           *APIv2Handler
	csrfLoginPath   string
	authExemptPaths map[string]struct{}
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
	g.Use(func(c *gin.Context) {
		if _, exempt := a.authExemptPaths[c.Request.URL.Path]; !exempt {
			checkLogin(c)
		}
	})
	g.Use(a.csrfMiddleware)
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
	}
}

func (a *APIHandler) registerGroupedRoutes(g *gin.RouterGroup) {
	authDeps := a.authDeps()
	authDeps.CSRF = a.ApiService.GetCSRF
	authDeps.ReloadTokensAfter = a.reloadTokensAfter
	authhttp.RegisterRoutes(g, authDeps)

	configDeps := a.configDeps()
	configDeps.LoginUser = GetLoginUser
	confighttp.RegisterRoutes(g, configDeps)

	dbtransferhttp.RegisterRoutes(g, a.dbTransferDeps())
	importxuihttp.RegisterRoutes(g, a.importXUIDeps())
	telemetryhttp.RegisterRoutes(g, a.telemetryDeps())
	updatehttp.RegisterRoutes(g, a.updateDeps())
	failoverhttp.RegisterRoutes(g, failoverhttp.Deps{
		Status:  service.FailoverStatusEntries,
		JSONObj: jsonObj,
	})

	remotesubhttp.RegisterRoutes(g, remotesubhttp.Deps{
		Service:        &a.RemoteOutboundService,
		RequireScope:   a.requireTokenScopeAny,
		Actor:          requestActor,
		ValidateTarget: confighttp.ValidateOutboundCheckTarget,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	})
	telegramhttp.RegisterRoutes(g, telegramhttp.Deps{
		Settings:       a.SettingService,
		Telegram:       a.TelegramService,
		AuditService:   a.AuditService,
		RequireScope:   a.requireTokenScopeAny,
		Actor:          requestActor,
		RemoteIP:       getRemoteIp,
		CheckRateLimit: checkTelegramBackupManualRateLimit,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
	})

	realtimehttp.RegisterRoutes(g, realtimehttp.Deps{
		SettingService: a.SettingService,
		LoginUser:      GetLoginUser,
		RemoteIP:       getRemoteIp,
		Scope:          realtimeScopeFromContext,
		Audit:          a.recordAudit,
		JSONObj:        jsonObj,
		JSONMsg:        jsonMsg,
	})

	// Experimental Paid Subscriptions module owns its own routes; mount them on
	// the already-authenticated (session + CSRF) browser group.
	paidadmin.RegisterRoutes(g, paidadmin.Deps{
		LoginUser: GetLoginUser,
		Audit:     a.ApiService.recordAudit,
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
