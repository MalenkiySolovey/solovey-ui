package bind

import (
	"net"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

type ListenResult struct {
	Listener      net.Listener
	RequestedAddr string
	FallbackAddr  string
	Fallback      bool
	BindError     error
}

func ListenWithFallbackResult(addr, host string, port string) (ListenResult, error) {
	result := ListenResult{RequestedAddr: addr}
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		result.Listener = listener
		return result, nil
	}
	if !shouldFallback(err) || host == "" {
		return result, err
	}
	fallback := net.JoinHostPort("127.0.0.1", port)
	logger.Warningf(
		"could not bind on %s (%v); falling back to loopback %s. Update the listen address from the UI to silence this warning.",
		addr, err, fallback,
	)
	listener, fallbackErr := net.Listen("tcp", fallback)
	if fallbackErr != nil {
		return result, fallbackErr
	}
	result.Listener = listener
	result.FallbackAddr = fallback
	result.Fallback = true
	result.BindError = err
	return result, nil
}

// shouldFallback reports whether err is the kind of bind error that points
// at a stale listen address inherited from another machine (the address is
// syntactically valid but the kernel does not own it).
func shouldFallback(err error) bool {
	return isAddrNotAvailable(err)
}
