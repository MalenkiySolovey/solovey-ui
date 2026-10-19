package planner

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
)

func TestRollbackAdmissionRequiresExactLocalOwnedIdentity(t *testing.T) {
	intent := RollbackIntent{HostRef: hostref.Local(), Owner: "server-protection", ResourceRef: ProtectionResourceRef,
		CurrentRevision: hash("current"), CurrentSemantic: hash("rules"), CurrentMembership: hash("members"), CheckpointDigest: hash("before-image")}
	if err := CheckRollback(intent); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RollbackIntent){
		func(value *RollbackIntent) { value.HostRef.ID = "remote" },
		func(value *RollbackIntent) { value.ResourceRef = "external-table" },
		func(value *RollbackIntent) { value.CurrentSemantic = "" },
		func(value *RollbackIntent) { value.CurrentMembership = "" },
		func(value *RollbackIntent) { value.CheckpointDigest = "unknown" },
	} {
		changed := intent
		change(&changed)
		requireReason(t, CheckRollback(changed), InvalidIntent)
	}
}
