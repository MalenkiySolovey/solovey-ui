//go:build !linux

package privilegedbroker

import (
	"errors"
	"net"
)

func verifyServerConnection(net.Conn, string) error {
	return errors.New("privileged broker requires Linux peer credentials")
}
