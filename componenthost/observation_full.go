//go:build !minimal

package componenthost

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	auditsvc "github.com/MalenkiySolovey/solovey-ui/service/audit"
	observationsvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
)

// ObservationPorts uses the actual owner interfaces, not SDK copies. Minimal
// builds omit this optional feature and its implementation dependencies.
type ObservationPorts struct {
	ObservationSampler observationsvc.Sampler
	ObservationHistory observationsvc.History
	AuditHistory       auditsvc.History
}

// WithObservationOwners belongs to core composition. Binding never constructs
// a service or falls back to ambient Runtime when a capability is absent.
func (d APIDeps) WithObservationOwners() APIDeps {
	if d.Runtime == nil {
		return d
	}
	owner := &service.ObservabilityService{ServerService: service.NewServerService(d.Runtime)}
	d.ObservationSampler, d.ObservationHistory = owner, owner
	d.AuditHistory = &service.AuditService{Runtime: d.Runtime}
	return d
}

func (d APIDeps) bindObservationPort(port string, target *Ports) bool {
	switch port {
	case manifest.PortObservationSample:
		target.ObservationSampler = d.ObservationSampler
		return target.ObservationSampler != nil
	case manifest.PortObservationHistory:
		target.ObservationHistory = d.ObservationHistory
		return target.ObservationHistory != nil
	case manifest.PortAuditHistory:
		target.AuditHistory = d.AuditHistory
		target.Auth.RequireAuditAdminScope = d.Auth.RequireAuditAdminScope
		target.Rate.CheckAuditRateLimit = d.Rate.CheckAuditRateLimit
		target.Rate.AuditRateLimitKey = d.Rate.AuditRateLimitKey
		target.Rate.AuditRateLimitWindow = d.Rate.AuditRateLimitWindow
		return target.AuditHistory != nil && target.Auth.RequireAuditAdminScope != nil &&
			target.Rate.CheckAuditRateLimit != nil && target.Rate.AuditRateLimitKey != nil
	}
	return false
}
