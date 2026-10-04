package ipmonitor

// Observer adapts the package IP policy to core/tracker without making either
// package import the other.
type Observer struct{}

func (Observer) ObserveAndAllow(clientName, ip string) bool {
	return ObserveAndAllow(clientName, ip)
}
