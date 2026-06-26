package ipmonitor

// Observer adapts the package IP policy to core/tracker without making either
// package import the other.
type Observer struct{}

func (Observer) Allow(clientName, ip string) bool {
	return Allow(clientName, ip)
}

func (Observer) Record(clientName, ip string) {
	Record(clientName, ip)
}
