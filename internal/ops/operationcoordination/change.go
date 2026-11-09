package operationcoordination

import (
	canonical "github.com/MalenkiySolovey/solovey-host/operationref"
	"strings"
)

type Reference = canonical.Reference

func NewReference(owner, resourceID, operationID string) Reference {
	return canonical.NewReference(owner, resourceID, operationID)
}

type Outcome string

const (
	OutcomePending          Outcome = "PENDING"
	OutcomeCommitted        Outcome = "COMMITTED"
	OutcomeFailed           Outcome = "FAILED"
	OutcomeRolledBack       Outcome = "ROLLED_BACK"
	OutcomeRecoveryRequired Outcome = "RECOVERY_REQUIRED"
)

// Problem carries only owner-controlled codes and references. It intentionally
// has no arbitrary payload/error field that could copy secrets into reports.
type Problem struct {
	Owner            string `json:"owner"`
	ResourceID       string `json:"resourceId"`
	Phase            string `json:"phase"`
	Code             string `json:"code"`
	RecoveryRequired bool   `json:"recoveryRequired"`
}

// Change projects the semantic lifecycle; it never interprets owner payloads
// or decides compensation. ExpectedRevision keeps the owner's existing binding
// (for example a restore rehearsal), not a new cross-owner authority token.
type Change struct {
	Reference
	Revision         uint64   `json:"revision"`
	ExpectedRevision string   `json:"expectedRevision,omitempty"`
	Phase            string   `json:"phase"`
	OwnerPhase       string   `json:"ownerPhase"`
	Outcome          Outcome  `json:"outcome"`
	CheckpointRef    string   `json:"checkpointRef,omitempty"`
	RollbackVerified bool     `json:"rollbackVerified"`
	RecoveryRequired bool     `json:"recoveryRequired"`
	Problem          *Problem `json:"problem,omitempty"`
}

func (c Change) WithProblem(code string) Change {
	if code != "" {
		if len(code) > 96 || strings.Trim(code, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			code = "operation_reason_invalid"
		}
		c.Problem = &Problem{Owner: c.Owner, ResourceID: c.ResourceID, Phase: c.Phase, Code: code, RecoveryRequired: c.RecoveryRequired}
	}
	return c
}

func referenceToken(value string, limit int) bool { return canonical.ValidToken(value, limit) }
