// Package planner coordinates Solovey-owned local network resources. Feature
// owners supply revisions and policy; this package has no shell or state store.
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

const Schema = "solovey-ui/network-plan/v1"

// The initial allocator owns the existing coexistence table only. There is no
// arbitrary table, mark, queue, route, command or global host reset operation.
const ProtectionAllocation = "inet:solovey_protection"

type Reason string

const (
	InvalidIntent         Reason = "INVALID_INTENT"
	StalePlan             Reason = "STALE_PLAN"
	ResourceConflict      Reason = "RESOURCE_CONFLICT"
	ResourceGone          Reason = "RESOURCE_GONE"
	EndpointChanged       Reason = "ENDPOINT_CHANGED"
	CapabilityUnavailable Reason = "CAPABILITY_UNAVAILABLE"
	ObservationUnknown    Reason = "OBSERVATION_UNKNOWN"
	ManagementUnknown     Reason = "MANAGEMENT_CONTINUITY_UNKNOWN"
	ManagementBlocked     Reason = "MANAGEMENT_CONTINUITY_BLOCKED"
	CandidateChanged      Reason = "CANDIDATE_CHANGED"
	AllocationUnavailable Reason = "ALLOCATION_UNAVAILABLE"
)

type Conflict struct {
	Reason      Reason `json:"reason"`
	ResourceRef string `json:"resourceRef"`
	Allocation  string `json:"allocation,omitempty"`
}

func (c *Conflict) Error() string { return fmt.Sprintf("network plan %s: %s", c.Reason, c.ResourceRef) }

// ResourceBinding references a canonical owner resource. EndpointRevision is
// the owner's exact effective socket identity, never a primary copied port ID.
type ResourceBinding struct {
	ResourceRef      string `json:"resourceRef"`
	Owner            string `json:"owner"`
	DesiredRevision  string `json:"desiredRevision"`
	EndpointRevision string `json:"endpointRevision"`
	Management       bool   `json:"management"`
}

type Intent struct {
	HostRef               hostref.Ref       `json:"hostRef"`
	Owner                 string            `json:"owner"`
	ResourceRef           string            `json:"resourceRef"`
	DesiredRevision       string            `json:"desiredRevision"`
	ConfigurationRevision string            `json:"configurationRevision,omitempty"`
	InputRevision         string            `json:"inputRevision"`
	Purpose               string            `json:"purpose"`
	Resources             []ResourceBinding `json:"resources"`
}

type Ownership struct {
	HostRef      hostref.Ref `json:"hostRef"`
	Owner        string      `json:"owner"`
	ResourceRef  string      `json:"resourceRef"`
	PlanRevision string      `json:"planRevision"`
	Kind         string      `json:"kind"`
	Purpose      string      `json:"purpose"`
	Allocation   string      `json:"allocation"`
}

// Allocation is a current platform observation. Proven must come from the
// adapter's managed-resource grammar/identity check, never from a familiar name.
type Allocation struct {
	Ownership
	Proven bool `json:"proven"`
}

type Continuity struct {
	ResourceRef      string `json:"resourceRef"`
	EndpointRevision string `json:"endpointRevision"`
	Status           string `json:"status"` // VERIFIED, BLOCKED, UNKNOWN
}

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
}

// Plan is immutable across the public boundary. Accessors return copies.
// No serialization can reconstruct the private accepted candidate receipt.
type Plan struct {
	intent    Intent
	facts     Facts
	revision  string
	resources []Ownership
}

func (p Plan) Revision() string       { return p.revision }
func (p Plan) Intent() Intent         { return clone(p.intent) }
func (p Plan) Resources() []Ownership { return append([]Ownership(nil), p.resources...) }

func Compile(intent Intent, facts Facts) (Plan, error) {
	intent, facts = canonicalIntent(intent), canonicalFacts(facts)
	if !intent.HostRef.IsLocal() || !token(intent.Owner, 64) || !token(intent.ResourceRef, 256) ||
		!digest(intent.DesiredRevision) || intent.ConfigurationRevision != "" && !digest(intent.ConfigurationRevision) || !digest(intent.InputRevision) || !token(intent.Purpose, 96) || len(intent.Resources) > 4096 {
		return Plan{}, conflict(InvalidIntent, intent.ResourceRef)
	}
	if !facts.HostRef.IsLocal() || !digest(facts.Revision) || !facts.InventoryComplete ||
		len(facts.Resources) > 4096 || len(facts.Allocations) > 8192 || len(facts.Continuity) > 4096 {
		return Plan{}, conflict(ObservationUnknown, intent.ResourceRef)
	}
	if facts.Capability.Availability != platform.Available || !token(facts.Capability.Backend, 96) || !digest(facts.CapabilityRevision) {
		return Plan{}, conflict(CapabilityUnavailable, intent.ResourceRef)
	}
	if err := checkBindings(intent, facts); err != nil {
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
	identity := struct {
		Schema string
		Intent Intent
		Facts  Facts
	}{Schema, intent, facts}
	plan := Plan{intent: intent, facts: facts, revision: revision(identity)}
	plan.resources = []Ownership{{HostRef: intent.HostRef, Owner: intent.Owner, ResourceRef: intent.ResourceRef,
		PlanRevision: plan.revision, Kind: "filter_table", Purpose: intent.Purpose, Allocation: ProtectionAllocation}}
	return plan, nil
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
	next, err := Compile(plan.intent, current)
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

type DriftKind string

const (
	Missing                   DriftKind = "MISSING"
	Mismatch                  DriftKind = "MISMATCH"
	Stale                     DriftKind = "STALE"
	UnexpectedManagedResource DriftKind = "UNEXPECTED_MANAGED_RESOURCE"
	DependencyUnavailable     DriftKind = "DEPENDENCY_UNAVAILABLE"
	Unknown                   DriftKind = "UNKNOWN"
)

type Drift struct {
	Kind        DriftKind `json:"kind"`
	ResourceRef string    `json:"resourceRef"`
	Repairable  bool      `json:"repairable"`
}

// Compare is bounded and read-only. A late observation of another accepted
// plan cannot change the current generation's status or authorize repair.
func Compare(plan Plan, observationPlanRevision string, available bool, actual []Allocation) []Drift {
	ref := plan.intent.ResourceRef
	if !digest(plan.revision) || !available || len(actual) > 8192 {
		return []Drift{{Kind: DependencyUnavailable, ResourceRef: ref}}
	}
	if observationPlanRevision != plan.revision {
		return []Drift{{Kind: Stale, ResourceRef: ref}}
	}
	for _, allocation := range actual {
		if allocation.Allocation != ProtectionAllocation {
			continue
		}
		if !allocation.Proven || allocation.HostRef != plan.intent.HostRef || allocation.Owner != plan.intent.Owner || allocation.ResourceRef != ref {
			return []Drift{{Kind: Unknown, ResourceRef: ref}}
		}
		if allocation.PlanRevision != plan.revision {
			return []Drift{{Kind: Mismatch, ResourceRef: ref, Repairable: true}}
		}
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
