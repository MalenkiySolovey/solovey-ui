package enforcement

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/intent"
)

type ManagementExemption struct {
	EndpointID     string                          `json:"endpointId"`
	RecoveryPathID string                          `json:"recoveryPathId"`
	Key            hostresources.PublicEndpointKey `json:"key"`
	SourcePrefix   string                          `json:"sourcePrefix"`
	ExpiresAt      int64                           `json:"expiresAt"`
}

// Endpoint is the exact owner-resolved target of a feature contribution. It is
// not current inventory, a lease, an accepted plan or an authorization grant.
type Endpoint struct {
	ResourceID, Owner, ConfigurationRevision, EndpointRevision string
	Key                                                        hostresources.PublicEndpointKey
	Management                                                 bool
	Contributions                                              []EndpointContribution
	UDPNew                                                     bool
}

type IntentInput struct {
	Revision, InputRevision, Mode string
	Limits                        DynamicSetLimits
	Endpoints                     []Endpoint
	ManagementExemptions          []ManagementExemption
	AllowTCPPorts, AllowUDPPorts  []int
	GraylistCIDRs                 []string
}

// NetworkIntent translates feature-owned policy only. The existing Network
// Planner must check current facts and the broker must admit every operation.
func NetworkIntent(plan IntentInput) networkplan.ProtectionEnforcementIntent {
	intent := networkplan.ProtectionEnforcementIntent{
		Binding: networkplan.Intent{HostRef: hostref.Local(), Owner: "server-protection", ResourceRef: networkplan.ProtectionResourceRef,
			DesiredRevision: plan.Revision, InputRevision: plan.InputRevision, Purpose: "protection-enforcement"},
		ArtifactRevision: plan.Revision, Mode: plan.Mode,
		Limits:        networkplan.ProtectionLimits{MaxElements: plan.Limits.MaxElements, DefaultTTLSeconds: plan.Limits.DefaultTTLSeconds, MaxTTLSeconds: plan.Limits.MaxTTLSeconds},
		Rates:         networkplan.ProtectionRates{Gray: networkplan.PacketLimit{Rate: 5, Burst: 10}, Rate: networkplan.PacketLimit{Rate: 20, Burst: 40}, Quarantine: networkplan.PacketLimit{Rate: 2, Burst: 4}},
		AllowTCPPorts: append([]int(nil), plan.AllowTCPPorts...), AllowUDPPorts: append([]int(nil), plan.AllowUDPPorts...), GraylistCIDRs: append([]string(nil), plan.GraylistCIDRs...),
	}
	for _, endpoint := range plan.Endpoints {
		binding := networkplan.ResourceBinding{ResourceRef: endpoint.ResourceID, Owner: endpoint.Owner,
			DesiredRevision: endpoint.ConfigurationRevision, EndpointRevision: endpoint.EndpointRevision, Management: endpoint.Management}
		intent.Binding.Resources = append(intent.Binding.Resources, binding)
		value := networkplan.ProtectionEndpoint{Binding: binding, Key: endpoint.Key}
		if endpoint.UDPNew {
			value.UDPNew = &networkplan.PacketLimit{Rate: 200, Burst: 400}
		}
		for _, contribution := range endpoint.Contributions {
			class := map[domain.ResponseIntent]networkplan.SetClass{domain.IntentSoftGraylist: networkplan.GraySet, domain.IntentRateLimit: networkplan.RateSet,
				domain.IntentTemporaryQuarantine: networkplan.QuarantineSet, domain.IntentTemporaryBlock: networkplan.BlockSet}[contribution.Intent]
			value.Contributions = append(value.Contributions, networkplan.TimedSource{Subject: contribution.Subject, Class: class, ExpiresAt: contribution.ExpiresAt, TTLSeconds: contribution.TTLSeconds})
		}
		intent.Endpoints = append(intent.Endpoints, value)
	}
	for _, exemption := range plan.ManagementExemptions {
		intent.ManagementExemptions = append(intent.ManagementExemptions, networkplan.ProtectionExemption{Key: exemption.Key, SourcePrefix: exemption.SourcePrefix, ExpiresAt: exemption.ExpiresAt, RecoveryPathID: exemption.RecoveryPathID})
	}
	return intent
}
