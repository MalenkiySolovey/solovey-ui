//go:build !linux

package openwrt

func RefreshDatabaseDurabilityProof() (DatabaseDurabilityProofV3, error) {
	return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
}

func LoadDatabaseDurabilityProof() (DatabaseDurabilityProofV3, error) {
	return DatabaseDurabilityProofV3{}, ErrUnprovenDurableState
}

func InspectDatabaseDurableState(uint64) (DurableStateEvidence, error) {
	return DurableStateEvidence{}, ErrUnprovenDurableState
}
