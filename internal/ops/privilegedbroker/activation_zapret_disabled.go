//go:build !linux || minimal || !zapretproof

package privilegedbroker

import "errors"

const zapretDenyActivationEnabled = false

func closeInheritedZapretDeny(_ int) error {
	return errors.New("Zapret deny descriptor is not supported by this build")
}
