//go:build !minimal

package fallbackhtml

import (
	"context"
	"reflect"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
)

// fallbackHealthChecker invokes the runtime directly. It bypasses HTTP and the
// observation pipeline, so health checks cannot create probe observations.
type fallbackHealthChecker struct {
	runtime *fallbackservice.Runtime
	facts   fallbackservice.HostFacts
}

func (fallbackHealthChecker) ResourceID() string { return "component:fallback-html:active" }
func (fallbackHealthChecker) Matches(resourceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(resourceID), "component:fallback-html:site:")
}

func (c fallbackHealthChecker) Check(ctx context.Context) componenthealth.Result {
	if err := ctx.Err(); err != nil {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: "health_check_timeout"}
	}
	if c.runtime == nil {
		return componenthealth.Result{Status: componenthealth.StatusMissingCapability, Check: "fallback_runtime", FactCode: "fallback_runtime_unavailable"}
	}
	before := c.runtime.PublicationState()
	status := c.runtime.Status()
	if !status.Active {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: "fallback_site_inactive"}
	}
	facts := c.facts
	if facts == nil {
		facts = fallbackservice.BackendHostFacts()
	}
	// Standalone targets use the existing isolated route contract. Checking
	// them while a reservation transaction is open must not acquire a second
	// settings/database connection. Shared Panel health retains its host facts.
	report := c.runtime.Health(publicsurface.Context{AdminBasePath: "/app/"})
	if !before.ListenerReady {
		report = fallbackservice.NewWithHost(nil, c.runtime, facts).RuntimeHealth()
	}
	if !report.OK {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: fallbackHealthFact(report)}
	}
	after := c.runtime.PublicationState()
	if !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: "runtime_generation_changed"}
	}
	result := componenthealth.Result{ResourceID: before.Resource.ResourceID, Status: componenthealth.StatusOK, Check: "fallback_runtime", FactCode: "runtime_routes_ready"}
	return componenthealth.WithEvidence(result, id, componenthealth.RuntimeReady, "fallback-runtime", before.EvidenceRevision(), time.Now().UTC(), 30*time.Second)
}

func fallbackHealthFact(report fallbackservice.RuntimeHealth) string {
	if report.HomeStatus != 200 {
		return "fallback_home_unhealthy"
	}
	if report.NotFoundStatus != 404 {
		return "fallback_not_found_unhealthy"
	}
	if !report.AdminReserved || !report.ACMEReserved {
		return "fallback_reserved_route_conflict"
	}
	return "fallback_runtime_unhealthy"
}
