package api

import (
	"time"

	confighttp "github.com/MalenkiySolovey/solovey-ui/api/config"
	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	"github.com/MalenkiySolovey/solovey-backend/hostcontext"
	authhttp "github.com/MalenkiySolovey/solovey-backend/http/auth"
	bearerhttp "github.com/MalenkiySolovey/solovey-backend/http/bearer"
	"github.com/MalenkiySolovey/solovey-backend/http/databaseoperation"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/gin-gonic/gin"
)

type APIv2Handler struct {
	ApiService
	auth       *authhttp.Handler
	config     *confighttp.Handler
	db         *dbtransferhttp.Handler
	telemetry  *telemetryhttp.Handler
	loadTokens func() ([]byte, error)
	bearer     *bearerhttp.Handler
}

const (
	apiUsernameKey          = bearerhttp.UsernameKey
	apiTokenScopeKey        = bearerhttp.ScopeKey
	legacyTokenHeaderSunset = bearerhttp.LegacyHeaderSunset
)

var apiTokenNow = time.Now

func NewAPIv2Handler(g *gin.RouterGroup, options ...Option) *APIv2Handler {
	a := &APIv2Handler{
		ApiService: NewApiService(options...),
	}
	a.auth = a.authHandler()
	a.config = a.configHandler()
	a.db = a.dbTransferHandler()
	a.db.AuthorizationChanged = a.ReloadTokens
	a.telemetry = a.coreTelemetryHandler()
	a.loadTokens = a.auth.LoadTokens
	a.bearer = bearerhttp.New(bearerhttp.Deps{
		Users:        &a.UserService,
		LoadTokens:   func() ([]byte, error) { return a.loadTokens() },
		Now:          func() time.Time { return apiTokenNow() },
		ClientIP:     getRemoteIp,
		Audit:        a.recordAudit,
		RequireScope: a.ApiService.requireTokenScopeAny,
		Warning:      logger.Warning,
		Error:        logger.Error,
	})
	a.ReloadTokens()
	a.initRouter(g)
	return a
}

func (a *APIv2Handler) initRouter(g *gin.RouterGroup) {
	g.Use(hostcontext.Middleware())
	g.Use(databaseoperation.Middleware())
	g.Use(func(c *gin.Context) {
		a.checkToken(c)
	})
	g.GET("/security/audit/recent", a.telemetry.GetRecentSecurityAudit)
	g.POST("/rotateSubSecret", a.config.RotateSubSecret)
	// Browser step-up grants are deliberately unavailable to bearer-token
	// component routes. Sensitive component mutations therefore fail closed.
	a.registerComponentAPIRoutes(g, a.componentAPI(nil, a.ReloadTokens))
	g.GET("/logs/entries", a.telemetry.GetLogEntries)
	g.GET("/diagnostics/report", a.telemetry.GetDiagnosticsReport)
	g.GET("/diagnostics/bundle", a.telemetry.GetDiagnosticsBundle)
	a.registerRuntimeRoutes(g)
	g.POST("/:postAction", a.postHandler)
	g.GET("/:getAction", a.getHandler)
}

func (a *APIv2Handler) enforceActionScope(c *gin.Context, action string) bool {
	return a.bearer.EnforceActionScope(c, action)
}

func (a *APIv2Handler) postHandler(c *gin.Context) {
	username := c.GetString(apiUsernameKey)
	action := c.Param("postAction")
	if !a.enforceActionScope(c, action) {
		return
	}

	switch action {
	case "save":
		a.config.Save(c, username)
	case "reorder":
		a.config.Reorder(c, username)
	case "restartApp":
		a.config.RestartApp(c)
	case "restartSb":
		a.config.RestartSb(c)
	case "linkConvert":
		a.config.LinkConvert(c)
	case "subConvert":
		a.config.SubConvert(c)
	case "getCertPing":
		a.config.GetCertPing(c)
	case "importdb":
		a.db.ImportDb(c)
	case "rotateSubSecret":
		a.config.RotateSubSecret(c)
	case "resetTraffic":
		a.config.ResetTraffic(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) getHandler(c *gin.Context) {
	action := c.Param("getAction")
	if !a.enforceActionScope(c, action) {
		return
	}

	switch action {
	case "load":
		a.config.LoadData(c)
	case "capabilities":
		a.config.GetCapabilities(c)
	case "inbounds", "inboundDrafts", "outbounds", "endpoints", "services", "tls", "clients", "config":
		err := a.config.LoadPartialData(c, []string{action})
		if err != nil {
			jsonMsg(c, action, err)
		}
		return
	case "users":
		a.auth.GetUsers(c)
	case "settings":
		a.config.GetSettings(c)
	case "stats":
		a.telemetry.GetStats(c)
	case "status":
		a.telemetry.GetStatus(c)
	case "onlines":
		a.telemetry.GetOnlines(c)
	case "logs":
		a.telemetry.GetLogs(c)
	case "changes":
		a.config.CheckChanges(c)
	case "keypairs":
		a.telemetry.GetKeypairs(c)
	case "getdb":
		a.db.DownloadDatabase(c)
	case "checkOutbound":
		a.config.GetCheckOutbound(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIv2Handler) checkToken(c *gin.Context) { a.bearer.CheckToken(c) }
func (a *APIv2Handler) ReloadTokens() {
	if a.bearer != nil {
		a.bearer.ReloadTokens()
	}
}
