//go:build !linux

package privilegedbroker

import (
	"errors"
)

func openStandaloneListeners(string, uint32) (*ListenerSet, error) {
	return nil, errors.New("standalone broker transport requires Linux")
}
