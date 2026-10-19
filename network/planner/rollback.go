package planner

import "github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"

// RollbackIntent describes compensation of proven owned state, not a new
// forward policy. The existing broker/checkpoint owner must still authenticate
// the before-image and compare the complete live identity before restoring it.
type RollbackIntent struct {
	HostRef                                                                                   hostref.Ref
	Owner, ResourceRef, CurrentRevision, CurrentSemantic, CurrentMembership, CheckpointDigest string
}

func CheckRollback(intent RollbackIntent) error {
	if !intent.HostRef.IsLocal() || !token(intent.Owner, 64) || intent.ResourceRef != ProtectionResourceRef ||
		!digest(intent.CurrentRevision) || !digest(intent.CurrentSemantic) || !digest(intent.CurrentMembership) || intent.CheckpointDigest != "" && !digest(intent.CheckpointDigest) {
		return conflict(InvalidIntent, intent.ResourceRef)
	}
	return nil
}
