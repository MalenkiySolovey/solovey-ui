package firewall

import (
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

const (
	FirewallPlanSchemaV2           = "solovey-ui/endpoint-firewall-plan/v2"
	ModeCoexistenceEndpointManaged = "COEXISTENCE_ENDPOINT_MANAGED"
	DefaultDynamicSetSize          = 4096
	DefaultDynamicTTLSeconds       = 4 * 60 * 60
	MaximumDynamicTTLSeconds       = 24 * 60 * 60
)

type DynamicSetLimits struct {
	MaxElements       int `json:"maxElements"`
	DefaultTTLSeconds int `json:"defaultTtlSeconds"`
	MaxTTLSeconds     int `json:"maxTtlSeconds"`
}

type EndpointContribution struct {
	ContributionID string                `json:"contributionId"`
	ActionID       string                `json:"actionId,omitempty"`
	ActionIDs      []string              `json:"actionIds,omitempty"`
	DecisionID     string                `json:"decisionId,omitempty"`
	DecisionIDs    []string              `json:"decisionIds,omitempty"`
	RefCount       int                   `json:"refCount"`
	Subject        string                `json:"subject"`
	Intent         domain.ResponseIntent `json:"intent"`
	ExpiresAt      int64                 `json:"expiresAt"`
	TTLSeconds     int                   `json:"ttlSeconds"`
	SourceClass    string                `json:"sourceClass"`
	SourceClasses  []string              `json:"sourceClasses,omitempty"`
}

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

type ManagementExemption struct {
	EndpointID     string                          `json:"endpointId"`
	RecoveryPathID string                          `json:"recoveryPathId"`
	Key            hostresources.PublicEndpointKey `json:"key"`
	SourcePrefix   string                          `json:"sourcePrefix"`
	ExpiresAt      int64                           `json:"expiresAt"`
}

type StormLimit struct {
	Protocol string `json:"protocol"`
	Rate     int    `json:"rate"`
	Burst    int    `json:"burst"`
}

type FirewallPlan struct {
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
	OperatingSystem     string
}
