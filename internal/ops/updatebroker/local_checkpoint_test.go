package updatebroker

import "testing"

func TestLocalInstallerAndSignedRollbackAuthorityCannotBeMixed(t *testing.T) {
	local := LocalGenerationCheckpointV1{Kind: installerRootCheckpoint, Digest: updateDigest("local installer contents")}
	if !ValidRollbackTarget(0, "", local) || !ValidRollbackTarget(7, updateDigest("signed manifest"), LocalGenerationCheckpointV1{}) {
		t.Fatal("disjoint local and signed rollback targets were rejected")
	}
	for _, candidate := range []struct {
		sequence uint64
		digest   string
		local    LocalGenerationCheckpointV1
	}{
		{0, "", LocalGenerationCheckpointV1{}},
		{7, updateDigest("signed manifest"), local},
		{7, "", local},
		{0, updateDigest("signed manifest"), local},
		{0, "", LocalGenerationCheckpointV1{Kind: "unknown/v1", Digest: local.Digest}},
		{0, "", LocalGenerationCheckpointV1{Kind: installerRootCheckpoint}},
	} {
		if ValidRollbackTarget(candidate.sequence, candidate.digest, candidate.local) {
			t.Fatal("mixed, empty or unknown rollback authority was accepted", candidate.sequence, candidate.local.Kind)
		}
	}
}
