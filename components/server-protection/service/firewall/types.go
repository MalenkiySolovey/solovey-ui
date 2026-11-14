package firewall

import (
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/enforcement"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

const (
	FirewallPlanSchemaV2           = "solovey-ui/endpoint-firewall-plan/v2"
	ModeCoexistenceEndpointManaged = "COEXISTENCE_ENDPOINT_MANAGED"
	DefaultDynamicSetSize          = 4096
	DefaultDynamicTTLSeconds       = 4 * 60 * 60
	MaximumDynamicTTLSeconds       = 24 * 60 * 60
)

type DynamicSetLimits = enforcement.DynamicSetLimits
type EndpointContribution = enforcement.EndpointContribution

type EndpointPolicy struct {
	EndpointRevision      string                          `json:"endpointRevision"`
	Key                   hostresources.PublicEndpointKey `json:"key"`
	ResourceID            string                          `json:"resourceId"`
	Owner                 string                          `json:"owner"`
	OwnerRevision         string                          `json:"ownerRevision"`
	ConfigurationRevision string                          `json:"configurationRevision"`
	Strategy              protectionresources.Strategy    `json:"strategy"`
	Management            bool                            `json:"management"`
	DesiredStatus         string                          `json:"desiredStatus"`
	SelectedStatus        string                          `json:"selectedStatus"`
	ActualStatus          string                          `json:"actualStatus"`
	Contributions         []EndpointContribution          `json:"contributions"`
	UDPFlowPolicy         *UDPFlowPolicyV1                `json:"udpFlowPolicy,omitempty"`
}

type ManagementExemption = enforcement.ManagementExemption

type StormLimit struct {
	Protocol string `json:"protocol"`
	Rate     int    `json:"rate"`
	Burst    int    `json:"burst"`
}

type FirewallPlan struct {
	// Only the planner supplies current mutation evidence. It is absent from
	// serialization and semantic identity: stored plans are recovery evidence,
	// never fresh authorization for another Apply.
	mutationEvidence         *firewallMutationEvidence
	Schema                   string                              `json:"schema,omitempty"`
	Mode                     string                              `json:"mode,omitempty"`
	Revision                 string                              `json:"revision"`
	InputRevision            string                              `json:"inputRevision,omitempty"`
	GraphRevision            string                              `json:"graphRevision,omitempty"`
	OwnerObservationRevision string                              `json:"ownerObservationRevision,omitempty"`
	BaselineEligibility      FirewallBaselineEligibility         `json:"firewallBaselineEligibility"`
	Resources                []hostresources.ProtectableResource `json:"resources"`
	Endpoints                []EndpointPolicy                    `json:"endpoints,omitempty"`
	ManagementExemptions     []ManagementExemption               `json:"managementExemptions,omitempty"`
	Limits                   DynamicSetLimits                    `json:"limits"`
	ApplyBlocked             bool                                `json:"applyBlocked"`
	ReasonCodes              []string                            `json:"reasonCodes,omitempty"`
	AllowTCPPorts            []int                               `json:"allowTcpPorts"`
	AllowUDPPorts            []int                               `json:"allowUdpPorts"`
	GraylistCIDRs            []string                            `json:"graylistCidrs"`
	StormLimits              []StormLimit                        `json:"stormLimits"`
	Warnings                 []string                            `json:"warnings"`
	ExplicitOpen             []string                            `json:"explicitOpen"`
}

type FirewallPreview struct {
	Revision      string                              `json:"revision"`
	InputRevision string                              `json:"inputRevision,omitempty"`
	Backend       string                              `json:"backend"`
	WouldKeep     []string                            `json:"wouldKeep"`
	WouldOpen     []string                            `json:"wouldOpen"`
	WouldWarn     []string                            `json:"wouldWarn"`
	WouldBlock    []string                            `json:"wouldBlock"`
	GeneratedNFT  string                              `json:"generatedNft,omitempty"`
	Warnings      []string                            `json:"warnings"`
	ProtectedKeep []hostresources.ProtectableResource `json:"protectedKeep"`
}

type PreviewOptions struct {
	IncludeGeneratedNFT bool
	NFTCapability       NFTPreviewCapability
}

// NFTPreviewCapability is the narrow helper-owned fact needed by preview.
// Preview never infers kernel support from the panel process platform.
type NFTPreviewCapability struct {
	Available bool
	Reason    string
	Revision  string
}
