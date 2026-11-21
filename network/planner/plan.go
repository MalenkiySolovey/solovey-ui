// Package planner coordinates Solovey-owned local network resources. Feature
// owners supply revisions and policy; this package has no shell or state store.
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	canonical "github.com/MalenkiySolovey/solovey-host/network/planning"
	"sort"
	"strings"

	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const Schema = canonical.Schema

// Allocation vocabulary is shared with typed requests; this package retains
// the single allocator and all accepted-plan authority.
const ProtectionAllocation = networkintent.ProtectionAllocation
const ProtectionFamily = networkintent.ProtectionFamily
const ProtectionTable = networkintent.ProtectionTable
const ProtectionTableCLI = networkintent.ProtectionTableCLI

type Reason = canonical.Reason

const (
	InvalidIntent         Reason = canonical.InvalidIntent
	StalePlan             Reason = canonical.StalePlan
	ResourceConflict      Reason = canonical.ResourceConflict
	ResourceGone          Reason = canonical.ResourceGone
	EndpointChanged       Reason = canonical.EndpointChanged
	CapabilityUnavailable Reason = canonical.CapabilityUnavailable
	ObservationUnknown    Reason = canonical.ObservationUnknown
	ManagementUnknown     Reason = canonical.ManagementUnknown
	ManagementBlocked     Reason = canonical.ManagementBlocked
	CandidateChanged      Reason = canonical.CandidateChanged
	AllocationUnavailable Reason = canonical.AllocationUnavailable
)

type Conflict = canonical.Conflict

type ResourceBinding = networkintent.ResourceBinding
type Intent = networkintent.Intent

type Ownership = canonical.Ownership

// Allocation is a current platform observation. Proven must come from the
// adapter's managed-resource grammar/identity check, never from a familiar name.
type Allocation = canonical.Allocation

type Continuity = canonical.Continuity

// Facts are operation-local inputs from existing owner/platform boundaries.
// The owner determines continuity policy. Revision changes invalidate preview,
// including a change that happens to produce identical platform bytes.
type Facts struct {
	HostRef            hostref.Ref           `json:"hostRef"`
	Revision           string                `json:"revision"`
	CapabilityRevision string                `json:"capabilityRevision"`
	Capability         platform.PlatformFact `json:"capability"`
	InventoryComplete  bool                  `json:"inventoryComplete"`
	Resources          []ResourceBinding     `json:"resources"`
	Allocations        []Allocation          `json:"allocations"`
	Continuity         []Continuity          `json:"continuity"`
	Zapret             *ZapretFacts          `json:"zapret,omitempty"`
}

// Plan is immutable across the public boundary. Accessors return copies.
// No serialization can reconstruct the private accepted candidate receipt.
type Plan struct {
	intent    Intent
	facts     Facts
	revision  string
	resources []Ownership
	zapret    *networkintent.ZapretPacketPathIntent
}

func (p Plan) Revision() string       { return p.revision }
func (p Plan) Intent() Intent         { return clone(p.intent) }
func (p Plan) Resources() []Ownership { return append([]Ownership(nil), p.resources...) }

func Compile(intent Intent, facts Facts) (Plan, error) {
	// Protection's existing plan identity does not depend on Zapret facts.
	facts.Zapret = nil
	intent, facts = canonicalIntent(intent), canonicalFacts(facts)
	if err := validatePlanInputs(intent, facts); err != nil {
		return Plan{}, err
	}
	for _, allocation := range facts.Allocations {
		if allocation.Allocation != ProtectionAllocation {
			continue
		}
		if !allocation.Proven || allocation.HostRef != intent.HostRef || allocation.Owner != intent.Owner || allocation.ResourceRef != intent.ResourceRef ||
			allocation.Kind != "filter_table" || !digest(allocation.PlanRevision) {
			return Plan{}, &Conflict{Reason: ResourceConflict, ResourceRef: intent.ResourceRef, Allocation: ProtectionAllocation}
		}
	}
	plan := newPlan(intent, facts)
	plan.resources = []Ownership{{HostRef: intent.HostRef, Owner: intent.Owner, ResourceRef: intent.ResourceRef,
		PlanRevision: plan.revision, Kind: "filter_table", Purpose: intent.Purpose, Allocation: ProtectionAllocation}}
	return plan, nil
}

func validatePlanInputs(intent Intent, facts Facts) error {
	if !intent.HostRef.IsLocal() || !token(intent.Owner, 64) || !token(intent.ResourceRef, 256) ||
		!digest(intent.DesiredRevision) || intent.ConfigurationRevision != "" && !digest(intent.ConfigurationRevision) || !digest(intent.InputRevision) || !token(intent.Purpose, 96) || len(intent.Resources) > 4096 {
		return conflict(InvalidIntent, intent.ResourceRef)
	}
	if !facts.HostRef.IsLocal() || !digest(facts.Revision) || !facts.InventoryComplete ||
		len(facts.Resources) > 4096 || len(facts.Allocations) > 8192 || len(facts.Continuity) > 4096 {
		return conflict(ObservationUnknown, intent.ResourceRef)
	}
	if facts.Capability.Availability != platform.Available || !token(facts.Capability.Backend, 96) || !digest(facts.CapabilityRevision) {
		return conflict(CapabilityUnavailable, intent.ResourceRef)
	}
	return checkBindings(intent, facts)
}

func newPlan(intent Intent, facts Facts) Plan {
	identity := struct {
		Schema string
		Intent Intent
		Facts  Facts
	}{Schema, intent, facts}
	return Plan{intent: intent, facts: facts, revision: revision(identity)}
}

func checkBindings(intent Intent, facts Facts) error {
	seen := make(map[string]bool, len(intent.Resources))
	for _, binding := range intent.Resources {
		key := binding.ResourceRef + "\x00" + binding.EndpointRevision
		if !token(binding.ResourceRef, 256) || !token(binding.Owner, 64) || !digest(binding.DesiredRevision) || !digest(binding.EndpointRevision) || seen[key] {
			return conflict(InvalidIntent, binding.ResourceRef)
		}
		seen[key] = true
		found, exact := false, false
		for _, current := range facts.Resources {
			if current.ResourceRef == binding.ResourceRef && current.Owner == binding.Owner {
				found = true
				if current == binding {
					exact = true
				}
			}
		}
		if !found {
			return conflict(ResourceGone, binding.ResourceRef)
		}
		if !exact {
			return conflict(EndpointChanged, binding.ResourceRef)
		}
		if binding.Management {
			verified := false
			for _, continuity := range facts.Continuity {
				if continuity.ResourceRef != binding.ResourceRef || continuity.EndpointRevision != binding.EndpointRevision {
					continue
				}
				if continuity.Status == "BLOCKED" {
					return conflict(ManagementBlocked, binding.ResourceRef)
				}
				verified = verified || continuity.Status == "VERIFIED"
			}
			if !verified {
				return conflict(ManagementUnknown, binding.ResourceRef)
			}
		}
	}
	return nil
}

// CheckCurrent is called again under the existing owner's operation fence,
// immediately before mutation. Recompilation never silently replaces a plan.
func CheckCurrent(plan Plan, current Facts) error {
	if !digest(plan.revision) {
		return conflict(InvalidIntent, "plan")
	}
	var next Plan
	var err error
	if plan.zapret != nil {
		candidate, compileErr := CompileZapret(*plan.zapret, current)
		next, err = candidate.plan, compileErr
	} else {
		next, err = Compile(plan.intent, current)
	}
	if err != nil {
		return err
	}
	if next.revision != plan.revision {
		return conflict(StalePlan, plan.intent.ResourceRef)
	}
	return nil
}

type Accepted struct {
	plan            Plan
	candidateDigest string
}

func (a Accepted) PlanRevision() string    { return a.plan.Revision() }
func (a Accepted) CandidateDigest() string { return a.candidateDigest }

// Accept records a successful narrow adapter validation of the exact artifact.
// It is not an authorization grant; the existing Change/broker owns admission.
func Accept(plan Plan, current Facts, validatedCandidateDigest string) (Accepted, error) {
	if err := CheckCurrent(plan, current); err != nil {
		return Accepted{}, err
	}
	if !digest(validatedCandidateDigest) {
		return Accepted{}, conflict(CandidateChanged, plan.intent.ResourceRef)
	}
	return Accepted{plan: plan, candidateDigest: validatedCandidateDigest}, nil
}

func CheckAccepted(accepted Accepted, current Facts, candidateDigest string) error {
	if accepted.candidateDigest == "" || accepted.candidateDigest != candidateDigest {
		return conflict(CandidateChanged, accepted.plan.intent.ResourceRef)
	}
	return CheckCurrent(accepted.plan, current)
}

type DriftKind = canonical.DriftKind

const (
	Missing                   DriftKind = canonical.Missing
	Mismatch                  DriftKind = canonical.Mismatch
	Stale                     DriftKind = canonical.Stale
	UnexpectedManagedResource DriftKind = canonical.UnexpectedManagedResource
	DependencyUnavailable     DriftKind = canonical.DependencyUnavailable
	Unknown                   DriftKind = canonical.Unknown
)

type Drift = canonical.Drift

// Compare is bounded and read-only. A late observation of another accepted
// plan cannot change the current generation's status or authorize repair.
func Compare(plan Plan, observationPlanRevision string, available bool, actual []Allocation) []Drift {
	ref := plan.intent.ResourceRef
	if !digest(plan.revision) || !available || len(actual) > 8192 {
		return []Drift{{Kind: DependencyUnavailable, ResourceRef: ref}}
	}
	return CompareResource(plan.resources[0], observationPlanRevision, available, actual)
}

// CompareResource is also used for existing durable owner seals. The caller
// fences the observation against that owner's current composition generation;
// a late sample never authorizes repair of a newer accepted plan.
func CompareResource(expected Ownership, observationPlanRevision string, available bool, actual []Allocation) []Drift {
	ref := expected.ResourceRef
	if !digest(expected.PlanRevision) || !available || len(actual) > 8192 {
		return []Drift{{Kind: DependencyUnavailable, ResourceRef: ref}}
	}
	if observationPlanRevision != expected.PlanRevision {
		return []Drift{{Kind: Stale, ResourceRef: ref}}
	}
	found := false
	for _, allocation := range actual {
		if allocation.Allocation != expected.Allocation {
			if strings.HasPrefix(allocation.Allocation, expected.Allocation+":") && allocation.Proven && allocation.HostRef == expected.HostRef && allocation.Owner == expected.Owner {
				return []Drift{{Kind: UnexpectedManagedResource, ResourceRef: allocation.ResourceRef}}
			}
			continue
		}
		if !allocation.Proven || allocation.HostRef != expected.HostRef || allocation.Owner != expected.Owner || allocation.ResourceRef != ref {
			return []Drift{{Kind: Unknown, ResourceRef: ref}}
		}
		if allocation.PlanRevision != expected.PlanRevision || allocation.IdentityMismatch {
			return []Drift{{Kind: Mismatch, ResourceRef: ref, Repairable: true}}
		}
		found = true
	}
	if found {
		return nil
	}
	return []Drift{{Kind: Missing, ResourceRef: ref, Repairable: true}}
}

func canonicalIntent(value Intent) Intent {
	value = clone(value)
	sort.Slice(value.Resources, func(i, j int) bool { return revision(value.Resources[i]) < revision(value.Resources[j]) })
	return value
}

func canonicalFacts(value Facts) Facts {
	value = clone(value)
	sort.Slice(value.Resources, func(i, j int) bool { return revision(value.Resources[i]) < revision(value.Resources[j]) })
	sort.Slice(value.Allocations, func(i, j int) bool { return revision(value.Allocations[i]) < revision(value.Allocations[j]) })
	sort.Slice(value.Continuity, func(i, j int) bool { return revision(value.Continuity[i]) < revision(value.Continuity[j]) })
	return value
}

func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func revision(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func digest(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
func token(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/@+-") == ""
}
func conflict(reason Reason, ref string) *Conflict {
	return &Conflict{Reason: reason, ResourceRef: ref}
}
