//go:build !linux

package privilegedbroker

import "net"

func preparePeerListener(*net.UnixListener) error { return nil }
func preparePeerConnection(*net.UnixConn) error   { return nil }

func readPeerRequest(connection *net.UnixConn, target *Request, limit int) (WriterCredentials, error) {
	return WriterCredentials{}, ReadFrame(connection, target, limit)
}
