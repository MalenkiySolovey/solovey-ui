//go:build linux

package privilegedbroker

// openStandaloneTransportAt is a package-local test seam. Production has no
// caller-controlled socket path surface.
func openStandaloneTransportAt(socketRoot string, socketGID uint32) (*ListenerSet, error) {
	return openStandaloneListeners(socketRoot, socketGID)
}
