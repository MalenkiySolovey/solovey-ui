package service

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"

	M "github.com/sagernet/sing/common/metadata"
)

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
	if err := coreInstance.ValidateOutbound(tag); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return coreInstance.DialOutbound(ctx, tag, network, M.ParseSocksaddr(addr))
		},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}
