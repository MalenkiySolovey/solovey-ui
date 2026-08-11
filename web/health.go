package web

import (
	"context"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
)

// panelHealthChecker is deliberately in-process: it confirms the configured
// panel listener without an HTTP request, cookies, paths, audit entries or
// public exposure.
type panelHealthChecker struct{ server *Server }

func (panelHealthChecker) ResourceID() string { return "core:panel:web" }

func (c panelHealthChecker) Check(ctx context.Context) componenthealth.Result {
	if err := ctx.Err(); err != nil {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, FactCode: "health_check_timeout"}
	}
	if c.server == nil || c.server.listener == nil || c.server.httpServer == nil || !c.server.running.Load() {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, FactCode: "panel_listener_unavailable"}
	}
	return componenthealth.Result{Status: componenthealth.StatusOK, Check: "panel_listener", FactCode: "listener_ready"}
}
