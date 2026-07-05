package ssrf

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const maxSafeRedirects = 5

var errBlockedDialAddress = common.NewError("url host resolves to a disallowed IP")

// NewHTTPClient returns an HTTP client that validates every request URL before
// it is sent and re-checks every dialed IP address. The dial-time check matters
// because a DNS answer can change between URL validation and the TCP connect.
func NewHTTPClient(timeout time.Duration, allowedSchemes ...string) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           safeDialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: safeRoundTripper{
			base:           transport,
			allowedSchemes: append([]string(nil), allowedSchemes...),
		},
		CheckRedirect: safeRedirectPolicy(allowedSchemes...),
	}
}

type safeRoundTripper struct {
	base           http.RoundTripper
	allowedSchemes []string
}

func (s safeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, common.NewError("missing request url")
	}
	if err := ValidateOutboundURL(request.Context(), request.URL.String(), s.allowedSchemes...); err != nil {
		return nil, err
	}
	base := s.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

func safeRedirectPolicy(allowedSchemes ...string) func(*http.Request, []*http.Request) error {
	allowed := append([]string(nil), allowedSchemes...)
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxSafeRedirects {
			return common.NewError("too many redirects")
		}
		if request == nil || request.URL == nil {
			return common.NewError("missing redirect url")
		}
		request.Header.Del("Authorization")
		request.Header.Del("Cookie")
		request.Header.Del("Referer")
		if len(via) > 0 && via[len(via)-1] != nil && via[len(via)-1].URL != nil &&
			via[len(via)-1].URL.Scheme == "https" && request.URL.Scheme == "http" {
			return common.NewError("redirect downgrades https to http")
		}
		return ValidateOutboundURL(request.Context(), request.URL.String(), allowed...)
	}
}

func safeDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if IsBlockedAddr(addr) {
			return nil, errBlockedDialAddress
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if ok {
			addr = addr.Unmap()
		}
		if !ok || IsBlockedAddr(addr) {
			lastErr = errBlockedDialAddress
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errBlockedDialAddress
	}
	return nil, lastErr
}
