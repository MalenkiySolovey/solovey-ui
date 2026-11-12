//go:build !minimal

package service

// Transient availability checks remain with the existing Backend network
// adapter. A free candidate grants no durable reservation or bind authority.
import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/network/bind"
	"net"
	"strconv"
	"strings"
)

func (backendHostFacts) PortAvailability(ctx context.Context, listen string, port int) (string, string) {
	if ctx.Err() != nil {
		return "unknown", "port availability check was cancelled"
	}
	return probeBind(listen, port)
}
func (backendHostFacts) FreeLoopbackPort(ctx context.Context, tls bool) (PortCandidate, bool) {
	if ctx.Err() != nil {
		return PortCandidate{}, false
	}
	return freeLoopbackPortCandidate(tls)
}
func (backendHostFacts) AddressFallback(ctx context.Context, listen string) (bool, string) {
	if ctx.Err() != nil {
		return false, ""
	}
	return addressWouldFallback(listen)
}
func (backendHostFacts) TLSConfigured(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	cert, key := runtimeTLSFiles()
	return cert != "" && key != ""
}

func freeLoopbackPortCandidate(tls bool) (PortCandidate, bool) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return PortCandidate{}, false
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return PortCandidate{}, false
	}
	return PortCandidate{
		Kind:    "free",
		Listen:  "127.0.0.1",
		Port:    addr.Port,
		Runtime: "gin",
		TLS:     tls,
		Status:  "free",
		Reason:  "available on loopback at the time of the check",
	}, true
}

func probeBind(listen string, port int) (string, string) {
	host := strings.TrimSpace(listen)
	if host == "" {
		host = "127.0.0.1"
	}
	result, err := bind.ListenWithFallbackResult(net.JoinHostPort(host, strconv.Itoa(port)), host, strconv.Itoa(port))
	if err != nil {
		return "blocked-external", "could not bind this port during a non-destructive availability check"
	}
	defer result.Listener.Close()
	if result.Fallback {
		return "loopback-fallback", "requested listen address is not available on this host; panel would fall back to loopback"
	}
	return "free", "available at the time of the check"
}

func addressWouldFallback(listen string) (bool, string) {
	host := strings.TrimSpace(listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return false, ""
	}
	result, err := bind.ListenWithFallbackResult(net.JoinHostPort(host, "0"), host, "0")
	if err != nil {
		return false, ""
	}
	defer result.Listener.Close()
	if result.Fallback {
		return true, "configured listen address is not available on this host; panel would fall back to loopback"
	}
	return false, ""
}
