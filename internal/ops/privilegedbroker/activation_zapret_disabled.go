//go:build !linux || minimal

package privilegedbroker

const zapretDenyActivationEnabled = false

type zapretDenyIdentity struct{}
