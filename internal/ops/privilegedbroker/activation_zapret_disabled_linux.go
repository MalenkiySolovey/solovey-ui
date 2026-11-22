//go:build linux && minimal

package privilegedbroker

import "errors"

func closeInheritedZapretDeny(_ int) (zapretDenyIdentity, error) {
	return zapretDenyIdentity{}, errors.New("zapret deny descriptor is not supported by this build")
}
