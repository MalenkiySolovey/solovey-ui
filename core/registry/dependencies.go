package registry

// Dependency identities stay beside registrations. Environment adapters project
// these identities; consumers do not maintain another protocol support table.
const (
	DependencyMulticast = "multicast"
	DependencyResolve1  = "resolve1-system-bus"
)
