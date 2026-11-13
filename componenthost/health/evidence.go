package health

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	"time"
)

type ProofKind = canonical.ProofKind
type Evidence = canonical.Evidence
type Requirement = canonical.Requirement

const (
	ConfigValid       = canonical.ConfigValid
	Applied           = canonical.Applied
	RuntimeReady      = canonical.RuntimeReady
	ProbeOK           = canonical.ProbeOK
	DataPlaneVerified = canonical.DataPlaneVerified
	EndToEndVerified  = canonical.EndToEndVerified
)

func Revision(value string) string { return canonical.Revision(value) }
func WithEvidence(result Result, owner string, proof ProofKind, source, observedRevision string, now time.Time, maxAge time.Duration) Result {
	return canonical.WithEvidence(result, owner, proof, source, observedRevision, now, maxAge)
}
func validEvidence(resourceID string, e *Evidence) bool {
	return canonical.ValidEvidence(resourceID, e)
}

// Require always executes the existing registry check. Cached evidence is not
// implicitly reused. Legacy checks without evidence remain usable through Check
// but cannot satisfy a revision/freshness-sensitive requirement.
func (r *Registry) Require(ctx context.Context, resourceID string, requirement Requirement) Result {
	result := r.Check(ctx, resourceID)
	if result.Evidence != nil && digest(requirement.DesiredRevision) {
		copy := *result.Evidence
		copy.DesiredRevision = requirement.DesiredRevision
		result.Evidence = &copy
	}
	if !result.Satisfies(requirement, time.Now()) && result.Status == StatusOK {
		result.Status, result.FactCode = StatusDegraded, "health_evidence_unsatisfied"
	}
	return result
}

func Require(ctx context.Context, resourceID string, requirement Requirement) Result {
	return Default.Require(ctx, resourceID, requirement)
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
