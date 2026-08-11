package firewall

import (
	"errors"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const UDPFlowPolicySchemaV1 = "solovey-ui/udp-flow-policy/v1"

// UDPFlowPolicyV1 is the exact bounded policy consumed by the managed
// firewall engine. Protocol owners retain auth, TLS, migration and session
// semantics; this contract never claims those responsibilities.
type UDPFlowPolicyV1 struct {
	Schema                       string                      `json:"schema"`
	ResourceID                   string                      `json:"resourceId"`
	EndpointID                   string                      `json:"endpointId"`
	AddressFamily                hostresources.AddressFamily `json:"addressFamily"`
	Protocol                     hostresources.Network       `json:"protocol"`
	ExactSocketRevision          string                      `json:"exactSocketRevision"`
	ManagementExclusionRevision  string                      `json:"managementExclusionRevision"`
	TrustedExclusionRevision     string                      `json:"trustedExclusionRevision"`
	RateProfile                  string                      `json:"rateProfile"`
	CardinalityProfile           string                      `json:"cardinalityProfile"`
	ConntrackPolicy              string                      `json:"conntrackPolicy"`
	ICMPPolicy                   string                      `json:"icmpPolicy"`
	ExpectedManagedTableRevision string                      `json:"expectedManagedTableRevision"`
	OperationRevision            string                      `json:"operationRevision"`
	PlanRevision                 string                      `json:"planRevision"`
	Revision                     string                      `json:"revision"`
}

func (p UDPFlowPolicyV1) Validate() error {
	if p.Schema != UDPFlowPolicySchemaV1 || p.Protocol != hostresources.NetworkUDP ||
		(p.AddressFamily != hostresources.AddressFamilyIPv4 && p.AddressFamily != hostresources.AddressFamilyIPv6) ||
		p.RateProfile != "BALANCED_V1" || p.CardinalityProfile != "BOUNDED_4096_V1" ||
		p.ConntrackPolicy != "ADVISORY_NEW_FLOW_V1" || p.ICMPPolicy != "PRESERVE_ICMP_AND_ICMPV6_V1" ||
		!validPolicyRevision(p.ExactSocketRevision) || !validPolicyRevision(p.ManagementExclusionRevision) || !validPolicyRevision(p.TrustedExclusionRevision) ||
		!validPolicyRevision(p.ExpectedManagedTableRevision) || !validPolicyRevision(p.OperationRevision) || !validPolicyRevision(p.PlanRevision) ||
		!validPolicyRevision(p.Revision) || p.Revision != hostresources.Revision(udpFlowPolicyRevisionInput(p)) {
		return errors.New("udp_flow_policy_v1_invalid")
	}
	return nil
}

func FinalizeUDPFlowPolicy(value UDPFlowPolicyV1) UDPFlowPolicyV1 {
	value.Schema = UDPFlowPolicySchemaV1
	value.Revision = hostresources.Revision(udpFlowPolicyRevisionInput(value))
	return value
}

func udpFlowPolicyRevisionInput(value UDPFlowPolicyV1) UDPFlowPolicyV1 {
	value.Revision = ""
	return value
}

func validPolicyRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
