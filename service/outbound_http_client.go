package service

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	M "github.com/sagernet/sing/common/metadata"
)

func NewOutboundHTTPClient(tag string, timeout time.Duration) (*http.Client, error) {
	return NewOutboundHTTPClientForRuntime(DefaultRuntime(), tag, timeout)
}

// NewOutboundHTTPClientForRuntime builds an HTTP client whose TCP connections
// are dialed through a running sing-box outbound by tag. Optional components
// use this when a request must egress through a configured proxy/VPN outbound.
func NewOutboundHTTPClientForRuntime(runtime *Runtime, tag string, timeout time.Duration) (*http.Client, error) {
	if tag == "" {
		return nil, common.NewError("outbound tag is empty")
	}
	runtime = runtimeOrDefault(runtime)
	coreInstance := runtime.Core()
	if coreInstance == nil || !coreInstance.IsRunning() {
		return nil, common.NewError("core is not running; cannot use outbound transport")
	}
	manager := coreInstance.OutboundManager()
	if manager == nil {
		return nil, common.NewError("core outbound manager unavailable")
	}
	ob, ok := manager.Outbound(tag)
	if !ok {
		return nil, common.NewErrorf("outbound not found: %s", tag)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return ob.DialContext(ctx, network, M.ParseSocksaddr(addr))
		},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}
