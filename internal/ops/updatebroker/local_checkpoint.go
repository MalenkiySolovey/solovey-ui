package updatebroker

// LocalGenerationCheckpointV1 binds an installed generation's Root-owned bytes.
// It conveys no signed release manifest, sequence or artifact provenance.
type LocalGenerationCheckpointV1 struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

const installerRootCheckpoint = "installer-root-owned/v1"

// ValidRollbackTarget keeps signed-release and local-generation authority
// disjoint. The Root owner additionally binds the local target's fixed name.
func ValidRollbackTarget(sequence uint64, digest string, local LocalGenerationCheckpointV1) bool {
	if local != (LocalGenerationCheckpointV1{}) {
		return sequence == 0 && digest == "" && local.Kind == installerRootCheckpoint && validDigest(local.Digest)
	}
	return sequence > 0 && validDigest(digest)
}
