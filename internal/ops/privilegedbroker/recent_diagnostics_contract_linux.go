//go:build linux && solovey_contract

package privilegedbroker

// OpenRecentDiagnosticRingAtForContract exposes the existing recorder only to
// the explicitly tagged local production-composition contract. Shipping
// builds retain the single fixed OpenRecentDiagnosticRing path.
func OpenRecentDiagnosticRingAtForContract(root string, ownerUID uint32) (*RecentDiagnosticRing, error) {
	return openRecentDiagnosticRingAt(root, ownerUID)
}

// ReadRecentDiagnosticsAtForContract exercises the same strict reader used by
// solovey-evidence recent without adding a runtime-selectable operator path.
func ReadRecentDiagnosticsAtForContract(path string, ownerUID uint32) (RecentDiagnostics, error) {
	return readRecentDiagnosticsAt(path, ownerUID)
}
