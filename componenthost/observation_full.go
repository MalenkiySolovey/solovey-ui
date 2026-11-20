//go:build !minimal

package componenthost

import (
	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-host/observation"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	auditsvc "github.com/MalenkiySolovey/solovey-ui/service/audit"
)

// ObservationPorts is filled by the selected Product composition, not Host.
type ObservationPorts struct {
	ObservationSampler observation.Sampler
	ObservationHistory observation.History
	AuditHistory       auditsvc.History
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

func (p Ports) bindLocalObservation(target *hostports.Ports) {
	target.ObservationSampler = p.ObservationSampler
}
