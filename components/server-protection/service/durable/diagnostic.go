package durable

// PersistenceDiagnostic is the storage owner's bounded, redacted failure
// attribution. It exposes no SQL, parameters, driver object or database handle.
type PersistenceDiagnostic struct {
	Step     string
	Role     string
	Primary  int
	Extended int
}

type PersistenceDiagnosticProvider interface {
	PersistenceDiagnostic() PersistenceDiagnostic
}
