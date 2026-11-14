package helpercontract

// ExecutionMetadata carries only identity revisions re-attested by the
// authorized helper client during the exact invocation.
type ExecutionMetadata struct {
	HelperIdentityRevision        string
	CapabilityRevision            string
	ListenerOwnerContractRevision string
	ListenerOwnerObserverRevision string
}
