package web

import (
	"context"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
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
	// Start publishes running only after listener/server initialization; Stop
	// clears it before retiring them. Do not race their mutable pointers here.
	if c.server == nil || !c.server.running.Load() {
		return componenthealth.Result{Status: componenthealth.StatusDegraded, FactCode: "panel_listener_unavailable"}
	}
	return componenthealth.WithEvidence(componenthealth.Result{ResourceID: c.ResourceID(), Status: componenthealth.StatusOK,
		Check: "panel_listener", FactCode: "listener_ready"}, "core", componenthealth.RuntimeReady, "panel_listener",
		componenthealth.Revision(configidentity.GetVersion()), time.Now(), 30*time.Second)
}
