package componenthost

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

// Deps is the in-process component host dependency surface.
//
// It is intentionally honest dependency injection, not a full capability
// sandbox: current in-process components still adapt legacy services and
// handlers that need the shared Runtime plus HTTP helper functions. APIDeps is
// split by responsibility so components can request auth, request metadata,
// rate-limit, audit, response, and update helpers explicitly instead of pulling
// a flat god-struct into every adapter.
type Deps struct {
	API       APIDeps
	Scheduler Scheduler
}

type Scheduler interface {
	AddJob(string, cron.Job) (cron.EntryID, error)
	Schedule(cron.Schedule, cron.Job) cron.EntryID
	RemoveJob(cron.EntryID)
	RemoveJobAndWait(context.Context, cron.EntryID) error
}

type APIDeps struct {
	Runtime *service.Runtime
	Auth    AuthDeps
	Request RequestDeps
	Rate    RateLimitDeps
	Audit   AuditDeps
	HTTP    HTTPDeps
	Update  UpdateDeps
}

type AuthDeps struct {
	RequireScope           func(*gin.Context, string, ...string) bool
	RequireAuditAdminScope func(*gin.Context) bool
	RequireStepUp          func(*gin.Context, string, string) bool
	LoginUser              func(*gin.Context) string
	CheckPassword          func(string, string, string) bool
}

type RequestDeps struct {
	Actor          func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	Hostname       func(*gin.Context) string
	ValidateTarget func(context.Context, string) error
}

type RateLimitDeps struct {
	CheckRateLimit        func(string) (time.Duration, error)
	CheckLoginRateLimit   func(string) error
	RecordLoginFailure    func(string)
	ResetLoginFailures    func(string)
	LoginRateLimitUserKey func(string) string
	CheckAuditRateLimit   func(string) error
	AuditRateLimitKey     func(string, string) string
	AuditRateLimitWindow  time.Duration
}

type AuditDeps struct {
	Audit func(*gin.Context, string, string, string, string, map[string]any)
}

type HTTPDeps struct {
	JSONObj func(*gin.Context, interface{}, error)
	JSONMsg func(*gin.Context, string, error)
}

type UpdateDeps struct {
	AllowForcedUpdateCheck func() bool
}

type HTTPRegisterFunc[T any] func(*gin.RouterGroup, T)

type RouteAdapter[T any] struct {
	Build    func(APIDeps) T
	Register HTTPRegisterFunc[T]
}

func (a RouteAdapter[T]) RegisterRoutes(g *gin.RouterGroup, host APIDeps) error {
	a.Register(g, a.Build(host))
	return nil
}
