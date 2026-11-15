// Package backendnetwork connects protection to the existing network owners.
package backendnetwork

import (
	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	contract "github.com/MalenkiySolovey/solovey-host/network/planning"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

type Planner struct{}

// The concrete owner handles stay private. Neither a serialized view nor a
// feature-defined implementation can reconstruct an accepted owner receipt.
type protectionPlan struct{ value planner.ProtectionPlan }

func (p protectionPlan) Revision() string                { return p.value.Revision() }
func (p protectionPlan) Intent() contract.Intent         { return p.value.Intent() }
func (p protectionPlan) Resources() []contract.Ownership { return p.value.Resources() }
func (p protectionPlan) Enforcement() networkintent.ProtectionEnforcementIntent {
	return p.value.Enforcement()
}

type acceptedPlan struct{ value planner.Accepted }

func (p acceptedPlan) PlanRevision() string    { return p.value.PlanRevision() }
func (p acceptedPlan) CandidateDigest() string { return p.value.CandidateDigest() }

func ownerFacts(value contract.Facts) planner.Facts {
	return planner.Facts{HostRef: value.HostRef, Revision: value.Revision, CapabilityRevision: value.CapabilityRevision,
		Capability:        platform.PlatformFact{Availability: platform.Availability(value.Capability.Availability), Backend: value.Capability.Backend, Reason: value.Capability.Reason},
		InventoryComplete: value.InventoryComplete, Resources: value.Resources, Allocations: value.Allocations, Continuity: value.Continuity}
}

func (Planner) CompileProtection(intent networkintent.ProtectionEnforcementIntent, facts contract.Facts) (contract.ProtectionPlan, error) {
	value, err := planner.CompileProtection(intent, ownerFacts(facts))
	if err != nil {
		return nil, err
	}
	return protectionPlan{value: value}, nil
}

func (Planner) Accept(value contract.ProtectionPlan, facts contract.Facts, digest string) (contract.Accepted, error) {
	owned, ok := value.(protectionPlan)
	if !ok {
		return nil, &contract.Conflict{Reason: contract.InvalidIntent, ResourceRef: contract.ProtectionResourceRef}
	}
	accepted, err := planner.Accept(owned.value.Plan, ownerFacts(facts), digest)
	if err != nil {
		return nil, err
	}
	return acceptedPlan{value: accepted}, nil
}

func (Planner) CheckAccepted(value contract.Accepted, facts contract.Facts, digest string) error {
	owned, ok := value.(acceptedPlan)
	if !ok {
		return &contract.Conflict{Reason: contract.CandidateChanged, ResourceRef: contract.ProtectionResourceRef}
	}
	return planner.CheckAccepted(owned.value, ownerFacts(facts), digest)
}

func (Planner) CompareResource(expected contract.Ownership, revision string, available bool, actual []contract.Allocation) []contract.Drift {
	return planner.CompareResource(expected, revision, available, actual)
}

func (Planner) CheckRollback(intent contract.RollbackIntent) error {
	return planner.CheckRollback(intent)
}
