package udpguard

import (
	"errors"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
)

const (
	UDPDirectGuardPlanSchemaV1 = "solovey-ui/udp-direct-guard-plan/v1"
	UDPStrategyHealthSchemaV1  = "solovey-ui/udp-strategy-health/v1"
	UDPStatusSchemaV1          = "solovey-ui/udp-direct-status/v1"
	MaxPlanReasonsV1           = 32
	MaxHealthFactsV1           = 4096
	MaxPlanFreshnessV1         = 5 * time.Minute
)

type ActualState string

const (
	StateNotApplied          ActualState = "NOT_APPLIED"
	StatePrepared            ActualState = "PREPARED"
	StateAppliedExperimental ActualState = "APPLIED_EXPERIMENTAL"
	StateDegraded            ActualState = "DEGRADED"
	StateBlocked             ActualState = "BLOCKED"
	StateRollingBack         ActualState = "ROLLING_BACK"
	StateRecoveryRequired    ActualState = "RECOVERY_REQUIRED"
	StateExternalManaged     ActualState = "EXTERNAL_MANAGED"
	StateUnsupported         ActualState = "UNSUPPORTED"
)

type ApplyGate string

const (
	ApplyGateExperimentalOff ApplyGate = "EXPERIMENTAL_DEFAULT_OFF"
	ApplyGateReady           ApplyGate = "EXPERIMENTAL_ACK_REQUIRED"
	ApplyGateBlocked         ApplyGate = "BLOCKED"
)

type UDPConfiguredSocketClaimV1 struct {
	ResourceID                  string                      `json:"resourceId"`
	EndpointID                  string                      `json:"endpointId"`
	ProviderID                  string                      `json:"providerId"`
	ProviderRevision            string                      `json:"providerRevision"`
	Protocol                    hostresources.Network       `json:"protocol"`
	AddressFamily               hostresources.AddressFamily `json:"addressFamily"`
	ConfiguredBind              string                      `json:"configuredBind"`
	Exposure                    string                      `json:"exposure"`
	Port                        uint16                      `json:"port"`
	SocketIntentRevision        string                      `json:"socketIntentRevision"`
	ConfigurationRevision       string                      `json:"configurationRevision"`
	RuntimeGenerationRevision   string                      `json:"runtimeGenerationRevision"`
	OwnerRevision               string                      `json:"ownerRevision"`
	ListenerObservationRevision string                      `json:"listenerObservationRevision"`
	ManagementExclusionRevision string                      `json:"managementExclusionRevision"`
	HealthRevision              string                      `json:"healthRevision"`
	ObservedAt                  int64                       `json:"observedAt"`
	ExpiresAt                   int64                       `json:"expiresAt"`
	ClaimRevision               string                      `json:"claimRevision"`
	ReasonCodes                 []string                    `json:"reasonCodes,omitempty"`
}

type UDPStrategyHealthV1 struct {
	Schema                   string                              `json:"schema"`
	ResourceID               string                              `json:"resourceId"`
	EndpointID               string                              `json:"endpointId"`
	StrategyClass            hostresources.InboundTransportClass `json:"strategyClass"`
	RuntimeReady             bool                                `json:"runtimeReady"`
	SocketObserved           bool                                `json:"socketObserved"`
	ManagedContributionReady bool                                `json:"managedContributionReady"`
	ProtocolTransactionReady bool                                `json:"protocolTransactionReady"`
	ManagementPreserved      bool                                `json:"managementPreserved"`
	CapacityReady            bool                                `json:"capacityReady"`
	RestartReconciled        bool                                `json:"restartReconciled"`
	ObservedAt               int64                               `json:"observedAt"`
	ExpiresAt                int64                               `json:"expiresAt"`
	Revision                 string                              `json:"revision"`
	ReasonCodes              []string                            `json:"reasonCodes,omitempty"`
}

type UDPDirectGuardPlanV1 struct {
	Schema                      string                              `json:"schema"`
	PlanID                      string                              `json:"planId"`
	PlanDigest                  string                              `json:"planDigest"`
	CreatedAt                   int64                               `json:"createdAt"`
	ExpiresAt                   int64                               `json:"expiresAt"`
	ResourceID                  string                              `json:"resourceId"`
	EndpointID                  string                              `json:"endpointId"`
	CapabilityRevision          string                              `json:"capabilityRevision"`
	BuildFeatureRevision        string                              `json:"buildFeatureRevision"`
	Claim                       UDPConfiguredSocketClaimV1          `json:"claim"`
	StrategyClass               hostresources.InboundTransportClass `json:"strategyClass"`
	DesiredPolicy               string                              `json:"desiredPolicy"`
	SelectedStrategy            string                              `json:"selectedStrategy"`
	ActualState                 ActualState                         `json:"actualState"`
	ApplyGate                   ApplyGate                           `json:"applyGate"`
	FirewallBaselineRevision    string                              `json:"firewallBaselineRevision"`
	ManagementExclusionRevision string                              `json:"managementExclusionRevision"`
	HealthRevision              string                              `json:"healthRevision"`
	FlowPolicy                  protectionfirewall.UDPFlowPolicyV1  `json:"flowPolicy"`
	SafetyRevision              string                              `json:"safetyRevision"`
	BlockCodes                  []string                            `json:"blockCodes,omitempty"`
	WarningCodes                []string                            `json:"warningCodes,omitempty"`
	LatestOperationID           string                              `json:"latestOperationId,omitempty"`
	LatestOperationRevision     int                                 `json:"latestOperationRevision,omitempty"`
	RecoveryRequired            bool                                `json:"recoveryRequired,omitempty"`
}

type CapabilityStatusV1 struct {
	ResourceID             string                              `json:"resourceId"`
	InboundType            string                              `json:"inboundType"`
	StrategyClass          hostresources.InboundTransportClass `json:"strategyClass"`
	ShippingStatus         string                              `json:"shippingStatus"`
	EffectiveNetworks      []hostresources.Network             `json:"effectiveNetworks"`
	Configured             bool                                `json:"configured"`
	Observed               bool                                `json:"observed"`
	DependentAssociation   bool                                `json:"dependentAssociation"`
	BuildFeatureState      hostresources.RuntimeFeatureState   `json:"buildFeatureState"`
	AuthenticationPresent  bool                                `json:"authenticationPresent"`
	TLSPresent             bool                                `json:"tlsPresent"`
	ProtocolOwnedZeroRTT   bool                                `json:"protocolOwnedZeroRtt"`
	ProtocolOwnedMigration bool                                `json:"protocolOwnedMigration"`
	ActualState            ActualState                         `json:"actualState"`
	ApplyGate              ApplyGate                           `json:"applyGate"`
	ReasonCodes            []string                            `json:"reasonCodes,omitempty"`
}

type StatusV1 struct {
	Schema              string                 `json:"schema"`
	GeneratedAt         int64                  `json:"generatedAt"`
	Capabilities        []CapabilityStatusV1   `json:"capabilities"`
	Plans               []UDPDirectGuardPlanV1 `json:"plans"`
	Experimental        bool                   `json:"experimental"`
	DefaultApplyEnabled bool                   `json:"defaultApplyEnabled"`
}

func (h UDPStrategyHealthV1) Ready(now time.Time) bool {
	return h.Validate(time.Time{}) == nil && h.ExpiresAt > now.UTC().Unix() &&
		h.RuntimeReady && h.SocketObserved && h.ManagedContributionReady && h.ProtocolTransactionReady &&
		h.ManagementPreserved && h.CapacityReady && h.RestartReconciled && digest(h.Revision) &&
		h.Revision == hostresources.Revision(healthRevisionInput(h)) && len(h.ReasonCodes) == 0
}

func (h UDPStrategyHealthV1) Validate(now time.Time) error {
	if h.Schema != UDPStrategyHealthSchemaV1 || h.ResourceID == "" || len(h.ResourceID) > 256 || h.EndpointID == "" || len(h.EndpointID) > 128 ||
		!healthStrategyClass(h.StrategyClass) ||
		h.ObservedAt <= 0 || h.ExpiresAt <= h.ObservedAt || h.ExpiresAt-h.ObservedAt > int64(MaxPlanFreshnessV1/time.Second) ||
		!now.IsZero() && h.ExpiresAt <= now.UTC().Unix() || !digest(h.Revision) ||
		h.Revision != hostresources.Revision(healthRevisionInput(h)) || !slices.Equal(reasons(h.ReasonCodes), h.ReasonCodes) {
		return errors.New("udp_strategy_health_v1_invalid")
	}
	return nil
}

func healthStrategyClass(value hostresources.InboundTransportClass) bool {
	switch value {
	case hostresources.TransportPlainUDP, hostresources.TransportTCPUDPDual,
		hostresources.TransportQUICNative, hostresources.TransportQUICV2Ray:
		return true
	default:
		return false
	}
}

func FinalizeHealth(value UDPStrategyHealthV1) UDPStrategyHealthV1 {
	value.Schema = UDPStrategyHealthSchemaV1
	value.ReasonCodes = reasons(value.ReasonCodes)
	value.Revision = hostresources.Revision(healthRevisionInput(value))
	return value
}

func healthRevisionInput(value UDPStrategyHealthV1) UDPStrategyHealthV1 {
	value.ObservedAt, value.ExpiresAt, value.Revision = 0, 0, ""
	return value
}

func claimRevision(value UDPConfiguredSocketClaimV1) string {
	value.ObservedAt, value.ExpiresAt, value.ClaimRevision = 0, 0, ""
	return hostresources.Revision(value)
}
func planDigest(value UDPDirectGuardPlanV1) string {
	value.PlanID, value.PlanDigest = "", ""
	value.CreatedAt, value.ExpiresAt = 0, 0
	value.LatestOperationID, value.LatestOperationRevision, value.RecoveryRequired = "", 0, false
	return hostresources.Revision(value)
}

func exactAddress(value string, family hostresources.AddressFamily) bool {
	address, err := netip.ParseAddr(value)
	if err != nil || address.Is4In6() || address.String() != value {
		return false
	}
	return family == hostresources.AddressFamilyIPv4 && address.Is4() || family == hostresources.AddressFamilyIPv6 && address.Is6()
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

func reasons(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" && len(value) <= 96 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	if len(result) > MaxPlanReasonsV1 {
		result = result[:MaxPlanReasonsV1]
	}
	return result
}
