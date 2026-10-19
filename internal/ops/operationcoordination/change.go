package operationcoordination

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

// Reference correlates projections without creating another durable operation.
// IDs are derived from an owner namespace and its existing opaque operation ID.
type Reference struct {
	CorrelationID string      `json:"correlationId"`
	ChangeID      string      `json:"changeId"`
	HostRef       hostref.Ref `json:"hostRef"`
	Owner         string      `json:"owner"`
	ResourceID    string      `json:"resourceId"`
	OperationID   string      `json:"operationId"`
}

func NewReference(owner, resourceID, operationID string) Reference {
	sum := sha256.Sum256([]byte(owner + "\x00" + operationID))
	id := hex.EncodeToString(sum[:24])
	if !referenceToken(owner, 64) || !referenceToken(resourceID, 256) || !referenceToken(operationID, 96) {
		owner, resourceID, operationID = "unavailable", "unavailable", "unavailable"
	}
	return Reference{CorrelationID: "operation:" + id, ChangeID: "change:" + id,
		HostRef: hostref.Local(), Owner: owner, ResourceID: resourceID, OperationID: operationID}
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

func referenceToken(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:@+-") == ""
}
