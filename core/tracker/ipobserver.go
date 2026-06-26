package tracker

// IPObserver is the policy boundary used by the connection tracker. Its
// implementation belongs outside core because enforcing and persisting client
// IP policy is application behavior, not sing-box connection accounting.
type IPObserver interface {
	Allow(clientName, ip string) bool
	Record(clientName, ip string)
}
