// Package telemetry owns operational read APIs and diagnostic actions.
package telemetry

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	StatsService           service.StatsService
	ServerService          service.ServerService
	DiagnosticsService     service.DiagnosticsService
	DoctorService          service.DoctorService
	ObservabilityService   service.ObservabilityService
	AuditService           service.AuditService
	VersionService         service.VersionService
	RequireScope           func(*gin.Context, string, ...string) bool
	JSONObj                func(*gin.Context, interface{}, error)
	JSONMsg                func(*gin.Context, string, error)
	Hostname               func(*gin.Context) string
	ValidateTarget         func(context.Context, string) error
	Audit                  func(*gin.Context, string, string, string, string, map[string]any)
	LoginUser              func(*gin.Context) string
	RequireAuditAdminScope func(*gin.Context) bool
	Actor                  func(*gin.Context) string
	RemoteIP               func(*gin.Context) string
	CheckAuditRateLimit    func(string) error
	AuditRateLimitKey      func(string, string) string
	AuditRateLimitWindow   time.Duration
}

// Deps contains the host capabilities required by telemetry routes.
type Deps struct {
	StatsService           service.StatsService
	ServerService          service.ServerService
	DiagnosticsService     service.DiagnosticsService
	DoctorService          service.DoctorService
	ObservabilityService   service.ObservabilityService
	AuditService           service.AuditService
	VersionService         service.VersionService
	RequireScope           func(*gin.Context, string, ...string) bool
	JSONObj                func(*gin.Context, interface{}, error)
	JSONMsg                func(*gin.Context, string, error)
	Hostname               func(*gin.Context) string
	ValidateTarget         func(context.Context, string) error
	Audit                  func(*gin.Context, string, string, string, string, map[string]any)
	LoginUser              func(*gin.Context) string
	RequireAuditAdminScope func(*gin.Context) bool
	Actor                  func(*gin.Context) string
	RemoteIP               func(*gin.Context) string
	CheckAuditRateLimit    func(string) error
	AuditRateLimitKey      func(string, string) string
	AuditRateLimitWindow   time.Duration
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		StatsService:           deps.StatsService,
		ServerService:          deps.ServerService,
		DiagnosticsService:     deps.DiagnosticsService,
		DoctorService:          deps.DoctorService,
		ObservabilityService:   deps.ObservabilityService,
		AuditService:           deps.AuditService,
		VersionService:         deps.VersionService,
		RequireScope:           deps.RequireScope,
		JSONObj:                deps.JSONObj,
		JSONMsg:                deps.JSONMsg,
		Hostname:               deps.Hostname,
		ValidateTarget:         deps.ValidateTarget,
		Audit:                  deps.Audit,
		LoginUser:              deps.LoginUser,
		RequireAuditAdminScope: deps.RequireAuditAdminScope,
		Actor:                  deps.Actor,
		RemoteIP:               deps.RemoteIP,
		CheckAuditRateLimit:    deps.CheckAuditRateLimit,
		AuditRateLimitKey:      deps.AuditRateLimitKey,
		AuditRateLimitWindow:   deps.AuditRateLimitWindow,
	}
}

// RegisterRoutes mounts operational data, diagnostics, and audit endpoints.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := NewHandler(deps)
	g.GET("/stats", h.GetStats)
	g.GET("/stats/traffic", h.GetTrafficStats)
	g.GET("/status", h.GetStatus)
	g.GET("/onlines", h.GetOnlines)
	g.GET("/keypairs", h.GetKeypairs)
	g.GET("/version", h.GetVersionInfo)
	g.GET("/logs", h.GetLogs)
	g.GET("/logs/entries", h.GetLogEntries)
	g.GET("/diagnostics/report", h.GetDiagnosticsReport)
	g.GET("/diagnostics/bundle", h.GetDiagnosticsBundle)

	doctor := g.Group("/doctor")
	doctor.POST("/run", h.RunDoctor)
	doctor.POST("/client", h.DiagnoseClient)

	security := g.Group("/security")
	security.GET("/audit", h.GetSecurityAudit)

	ipMonitor := g.Group("/ip-monitor")
	ipMonitor.GET("/:client", h.GetClientIPHistory)
	ipMonitor.POST("/:client/clear", h.ClearClientIPHistory)

	observability := g.Group("/observability")
	observability.GET("/history", h.GetObservabilityHistory)
	observability.GET("/core-history", h.GetCoreHistory)
}

type Envelope struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}
