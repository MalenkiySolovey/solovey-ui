//go:build linux && (minimal || !zapretproof)

package privilegedbroker

import "errors"

func closeInheritedZapretDeny(_ int) error {
	return errors.New("zapret deny descriptor is not supported by this build")
}
