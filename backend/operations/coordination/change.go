package operationcoordination

import canonical "github.com/MalenkiySolovey/solovey-host/operationref"

type Reference = canonical.Reference
type Outcome = canonical.Outcome
type Problem = canonical.Problem
type Change = canonical.Change

const (
	OutcomePending          = canonical.OutcomePending
	OutcomeCommitted        = canonical.OutcomeCommitted
	OutcomeFailed           = canonical.OutcomeFailed
	OutcomeRolledBack       = canonical.OutcomeRolledBack
	OutcomeRecoveryRequired = canonical.OutcomeRecoveryRequired
)

func NewReference(owner, resourceID, operationID string) Reference {
	return canonical.NewReference(owner, resourceID, operationID)
}
func referenceToken(value string, limit int) bool { return canonical.ValidToken(value, limit) }
