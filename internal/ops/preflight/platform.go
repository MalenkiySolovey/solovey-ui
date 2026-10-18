// Package preflight evaluates bounded facts. It never probes, authorizes,
// selects a platform backend or performs owner mutations.
package preflight

import (
	"context"
	"strings"
	"time"

	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

type Dimension string

const (
	Supervision       Dimension = "SERVICE_SUPERVISION"
	Privilege         Dimension = "PRIVILEGE_EXECUTION"
	UpdateLifecycle   Dimension = "UPDATE_LIFECYCLE"
	PersistentStorage Dimension = "PERSISTENT_STORAGE"
	PackageOperations Dimension = "PACKAGE_OPERATIONS"
)

// Requirements are selected by the semantic owner. Every listed prerequisite
// is critical: UNKNOWN, stale, malformed and cancelled observations fail closed.
type Requirement struct {
	Dimension Dimension
	Backend   string
}

type Check struct {
	Dimension    Dimension           `json:"dimension"`
	Availability domain.Availability `json:"availability"`
	ReasonCode   string              `json:"reasonCode"`
	Satisfied    bool                `json:"satisfied"`
}

type Report struct {
	Ready            bool    `json:"ready"`
	EvidenceRevision string  `json:"evidenceRevision,omitempty"`
	ObservedAt       int64   `json:"observedAt,omitempty"`
	ExpiresAt        int64   `json:"expiresAt,omitempty"`
	Checks           []Check `json:"checks"`
}

func Platform(ctx context.Context, facts domain.PlatformFacts, now time.Time, requirements ...Requirement) Report {
	result := Report{Ready: len(requirements) > 0 && len(requirements) <= 8, Checks: []Check{}}
	if len(requirements) > 8 {
		requirements = requirements[:8]
	}
	revision := facts.Revision
	facts.Revision = ""
	valid := facts.Schema == domain.PlatformFactsSchemaV1 && facts.HostScope == "LOCAL" &&
		facts.Source != "unavailable" && code(facts.Source) && digest(facts.EvidenceRevision) &&
		digest(revision) && revision == domain.Revision(facts) && facts.ObservedAt > 0 &&
		facts.ObservedAt <= now.Unix() && facts.ExpiresAt > now.Unix() &&
		facts.ExpiresAt-facts.ObservedAt <= int64((5*time.Minute)/time.Second)
	if valid {
		result.EvidenceRevision, result.ObservedAt, result.ExpiresAt = facts.EvidenceRevision, facts.ObservedAt, facts.ExpiresAt
	}
	for _, requirement := range requirements {
		check := Check{Dimension: requirement.Dimension, Availability: domain.Unknown, ReasonCode: "platform_evidence_invalid_or_stale"}
		fact, known := selectFact(facts, requirement.Dimension)
		if !known {
			check.Dimension = "UNKNOWN"
		}
		switch {
		case ctx == nil || ctx.Err() != nil:
			check.ReasonCode = "preflight_cancelled"
		case !known:
			check.ReasonCode = "preflight_dimension_unknown"
		case valid:
			if fact.Availability == domain.Available || fact.Availability == domain.Unavailable {
				check.Availability = fact.Availability
			}
			check.ReasonCode = "platform_prerequisite_unknown"
			if code(fact.Reason) {
				check.ReasonCode = fact.Reason
			}
			check.Satisfied = check.Availability == domain.Available && code(fact.Backend) &&
				(requirement.Backend == "" || fact.Backend == requirement.Backend)
			if check.Availability == domain.Available && !check.Satisfied {
				check.ReasonCode = "platform_backend_requirement_unsatisfied"
			}
		}
		result.Ready = result.Ready && check.Satisfied
		result.Checks = append(result.Checks, check)
	}
	return result
}

func selectFact(facts domain.PlatformFacts, dimension Dimension) (domain.PlatformFact, bool) {
	switch dimension {
	case Supervision:
		return facts.ServiceSupervision, true
	case Privilege:
		return facts.PrivilegeExecution, true
	case UpdateLifecycle:
		return facts.UpdateLifecycle, true
	case PersistentStorage:
		return facts.PersistentStorage, true
	case PackageOperations:
		return facts.PackageOperations, true
	default:
		return domain.PlatformFact{}, false
	}
}

func code(value string) bool {
	return value != "" && len(value) <= 96 && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:/-") == ""
}
func digest(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
