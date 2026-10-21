package componenthost

import (
	"context"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/gin-gonic/gin"
)

// Ports is the typed in-process boundary for migrated Components. It contains
// existing owner contracts, never a Runtime, database, filesystem or executor.
// Only declared ports are populated; Host is always the actual Local Host.
type Ports struct {
	Host              hostref.Ref
	Scheduler         Scheduler
	OutboundHTTP      OutboundHTTPClientProvider
	Auth              AuthDeps
	Request           RequestDeps
	Rate              RateLimitDeps
	Audit             AuditDeps
	HTTP              HTTPDeps
	DropOwnedSettings func(context.Context) error
}

func (d Deps) BindDataDrop(component manifest.Manifest) (Ports, error) {
	if err := component.Validate(); err != nil {
		return Ports{}, err
	}
	if component.HostAPI == nil {
		return Ports{}, fmt.Errorf("typed data-drop contract is required")
	}
	ports := Ports{Host: hostref.Local()}
	if len(component.HostAPI.DropData) != 0 {
		if d.DropOwnedSettings == nil || len(component.Database.Settings)+len(component.Database.Secrets) == 0 {
			return Ports{}, fmt.Errorf("component owned settings port is unavailable")
		}
		ports.DropOwnedSettings = func(ctx context.Context) error { return d.DropOwnedSettings(ctx, component.ID) }
	}
	return ports, nil
}

func (d Deps) BindLifecycle(contract manifest.HostAPI) (Ports, error) {
	if err := contract.Validate(); err != nil {
		return Ports{}, err
	}
	return d.bind(contract.Lifecycle)
}

func (d APIDeps) BindAPI(contract manifest.HostAPI) (Ports, error) {
	if err := contract.Validate(); err != nil {
		return Ports{}, err
	}
	return (Deps{API: d}).bind(contract.API)
}

func (d Deps) bind(required []string) (Ports, error) {
	ports := Ports{Host: hostref.Local()}
	for _, port := range required {
		available := false
		switch port {
		case manifest.PortScheduler:
			ports.Scheduler, available = d.Scheduler, d.Scheduler != nil
		case manifest.PortOutboundHTTP:
			ports.OutboundHTTP, available = d.API.OutboundHTTP, d.API.OutboundHTTP != nil
		case manifest.PortAuditRecord:
			ports.Audit.Recorder, available = d.API.Audit.Recorder, d.API.Audit.Recorder != nil
		case manifest.PortHTTPRequest:
			// Request handling does not imply credential checks, step-up grant
			// consumption, authorization mutation or arbitrary target validation.
			ports.Auth.RequireScope = d.API.Auth.RequireScope
			ports.Request.Actor, ports.Request.RemoteIP = d.API.Request.Actor, d.API.Request.RemoteIP
			ports.Rate.CheckRateLimit = d.API.Rate.CheckRateLimit
			ports.HTTP.JSONObj = d.API.HTTP.JSONObj
			ports.Audit.Audit = d.API.Audit.Audit
			available = ports.Auth.RequireScope != nil && ports.HTTP.JSONObj != nil
		}
		if !available {
			return Ports{}, fmt.Errorf("component host port %q is unavailable", port)
		}
	}
	return ports, nil
}

type PortRouteAdapter[T any] struct {
	Build    func(Ports) T
	Register HTTPRegisterFunc[T]
}

func (a PortRouteAdapter[T]) RegisterRoutes(g *gin.RouterGroup, ports Ports) error {
	a.Register(g, a.Build(ports))
	return nil
}
