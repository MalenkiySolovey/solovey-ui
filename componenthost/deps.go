package componenthost

import (
	"context"
	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-backend/componentroutes"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
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
	API               APIDeps
	Scheduler         Scheduler
	DropOwnedSettings func(context.Context, string) error
}

type Scheduler = hostports.Scheduler

type APIDeps struct {
	ObservationPorts
	Runtime           *service.Runtime
	ComponentSettings ComponentSettings
	OutboundHTTP      OutboundHTTPClientProvider
	Auth              AuthDeps
	Request           RequestDeps
	Rate              RateLimitDeps
	Audit             AuditDeps
	HTTP              HTTPDeps
	Update            UpdateDeps
}

type AuthDeps = componentroutes.AuthDeps
type RequestDeps = componentroutes.RequestDeps
type RateLimitDeps = componentroutes.RateLimitDeps
type AuditDeps = componentroutes.AuditDeps
type HTTPDeps = componentroutes.HTTPDeps
type UpdateDeps = componentroutes.UpdateDeps

type HTTPRegisterFunc[T any] func(*gin.RouterGroup, T)

type RouteAdapter[T any] struct {
	Build    func(APIDeps) T
	Register HTTPRegisterFunc[T]
}

func (a RouteAdapter[T]) RegisterRoutes(g *gin.RouterGroup, host APIDeps) error {
	a.Register(g, a.Build(host))
	return nil
}

// ComponentSettings is the existing Panel command used by lifecycle enable and
// compensation. It retains the ConfigService transaction and post-commit effects.
type ComponentSettings = componentroutes.ComponentSettings
