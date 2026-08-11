//go:build !minimal

package fallbackhtml

import (
	"context"
	"strings"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
)

// fallbackHealthChecker invokes the runtime directly. It bypasses HTTP and the
// observation pipeline, so health checks cannot create probe observations.
type fallbackHealthChecker struct{ runtime *fallbackservice.Runtime }

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
	status := c.runtime.Status()
	if !status.Active {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: "fallback_site_inactive"}
	}
	report := fallbackservice.New(nil, c.runtime).RuntimeHealth()
	if !report.OK {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, Check: "fallback_runtime", FactCode: fallbackHealthFact(report)}
	}
	return componenthealth.Result{Status: componenthealth.StatusOK, Check: "fallback_runtime", FactCode: "runtime_routes_ready"}
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
