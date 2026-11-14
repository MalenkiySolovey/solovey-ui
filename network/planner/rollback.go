package planner

import canonical "github.com/MalenkiySolovey/solovey-host/network/planning"

// RollbackIntent describes compensation of proven owned state, not a new
// forward policy. The existing broker/checkpoint owner must still authenticate
// the before-image and compare the complete live identity before restoring it.
type RollbackIntent = canonical.RollbackIntent

func CheckRollback(intent RollbackIntent) error {
	if !intent.HostRef.IsLocal() || !token(intent.Owner, 64) || intent.ResourceRef != ProtectionResourceRef ||
		!digest(intent.CurrentRevision) || !digest(intent.CurrentSemantic) || !digest(intent.CurrentMembership) || intent.CheckpointDigest != "" && !digest(intent.CheckpointDigest) {
		return conflict(InvalidIntent, intent.ResourceRef)
	}
	return nil
}
