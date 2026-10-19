package health

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

type ProofKind string

const (
	ConfigValid       ProofKind = "CONFIG_VALID"
	Applied           ProofKind = "APPLIED"
	RuntimeReady      ProofKind = "RUNTIME_READY"
	ProbeOK           ProofKind = "PROBE_OK"
	DataPlaneVerified ProofKind = "DATA_PLANE_VERIFIED"
	EndToEndVerified  ProofKind = "END_TO_END_VERIFIED"
)

// Evidence proves a specific proposition. Proof kinds have no numeric ordering.
// DesiredRevision is the consumer binding; ObservedRevision is supplied only by
// the semantic checker. An observation is never a repair or authorization grant.
type Evidence struct {
	HostRef          hostref.Ref `json:"hostRef"`
	Owner            string      `json:"owner"`
	ResourceID       string      `json:"resourceId"`
	ProofKind        ProofKind   `json:"proofKind"`
	Source           string      `json:"source"`
	DesiredRevision  string      `json:"desiredRevision,omitempty"`
	ObservedRevision string      `json:"observedRevision"`
	EvidenceRevision string      `json:"evidenceRevision"`
	ObservedAt       int64       `json:"observedAt"`
	MaxAgeSeconds    int64       `json:"maxAgeSeconds"`
	EvidenceRef      string      `json:"evidenceRef"`
}

type Requirement struct {
	Owner           string
	Source          string
	ProofKind       ProofKind
	DesiredRevision string
	MaxAge          time.Duration
}

func Revision(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func WithEvidence(result Result, owner string, proof ProofKind, source, observedRevision string, now time.Time, maxAge time.Duration) Result {
	evidence := &Evidence{HostRef: hostref.Local(), Owner: owner, ResourceID: result.ResourceID,
		ProofKind: proof, Source: source, ObservedRevision: observedRevision, ObservedAt: now.Unix(), MaxAgeSeconds: int64(maxAge / time.Second)}
	evidence.EvidenceRevision = Revision(owner + "\x00" + result.ResourceID + "\x00" + string(proof) + "\x00" + source + "\x00" +
		observedRevision + "\x00" + now.UTC().Format(time.RFC3339Nano) + "\x00" + string(result.Status) + "\x00" + result.FactCode)
	evidence.EvidenceRef = "health:" + evidence.EvidenceRevision
	result.Evidence = evidence
	return result
}

func (r Result) Satisfies(requirement Requirement, now time.Time) bool {
	e := r.Evidence
	return r.Status == StatusOK && validEvidence(r.ResourceID, e) &&
		healthToken(requirement.Owner, 64) && healthToken(requirement.Source, 64) && proofKind(requirement.ProofKind) &&
		digest(requirement.DesiredRevision) && requirement.MaxAge > 0 && requirement.MaxAge <= time.Minute &&
		e.Owner == requirement.Owner && e.Source == requirement.Source && e.ProofKind == requirement.ProofKind &&
		e.ObservedRevision == requirement.DesiredRevision && e.ObservedAt <= now.Unix() &&
		now.Unix()-e.ObservedAt <= e.MaxAgeSeconds && now.Sub(time.Unix(e.ObservedAt, 0)) <= requirement.MaxAge
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

func validEvidence(resourceID string, e *Evidence) bool {
	return e != nil && e.HostRef.IsLocal() && e.ResourceID == resourceID && healthToken(e.Owner, 64) &&
		healthToken(e.Source, 64) && proofKind(e.ProofKind) && digest(e.ObservedRevision) && digest(e.EvidenceRevision) &&
		(e.DesiredRevision == "" || digest(e.DesiredRevision)) && e.ObservedAt > 0 && e.MaxAgeSeconds > 0 && e.MaxAgeSeconds <= 60 &&
		e.EvidenceRef == "health:"+e.EvidenceRevision
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
func proofKind(kind ProofKind) bool {
	switch kind {
	case ConfigValid, Applied, RuntimeReady, ProbeOK, DataPlaneVerified, EndToEndVerified:
		return true
	default:
		return false
	}
}
