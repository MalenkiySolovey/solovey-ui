package firewall

import (
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/enforcement"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// protectionIntent binds the concrete owner plan to the reusable feature
// compiler. Inventory, management admission and all execution stay here.
func protectionIntent(plan FirewallPlan) networkplan.ProtectionEnforcementIntent {
	inputRevision := plan.InputRevision
	if inputRevision == "" {
		inputRevision = hostresources.Revision(CanonicalPlanResources(plan.Resources))
	}
	endpoints := make([]enforcement.Endpoint, 0, len(plan.Endpoints))
	for _, endpoint := range plan.Endpoints {
		endpoints = append(endpoints, enforcement.Endpoint{
			ResourceID: endpoint.ResourceID, Owner: endpoint.Owner,
			ConfigurationRevision: endpoint.ConfigurationRevision, EndpointRevision: endpoint.EndpointRevision,
			Key: endpoint.Key, Management: endpoint.Management, Contributions: endpoint.Contributions,
			UDPNew: endpoint.UDPFlowPolicy != nil,
		})
	}
	return enforcement.NetworkIntent(enforcement.IntentInput{
		Revision: plan.Revision, InputRevision: inputRevision, Mode: plan.Mode,
		Limits: plan.Limits, Endpoints: endpoints, ManagementExemptions: plan.ManagementExemptions,
		AllowTCPPorts: plan.AllowTCPPorts, AllowUDPPorts: plan.AllowUDPPorts, GraylistCIDRs: plan.GraylistCIDRs,
	})
}
