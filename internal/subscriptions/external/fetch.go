package external

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	subparser "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	uricodec "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

const (
	maxExternalSubBytes  = 4 << 20
	maxExternalRedirects = 5
)

var (
	externalHTTPClientOnce sync.Once
	externalHTTPClient     *http.Client
)

// errBlockedExternalAddress is returned by the dialer hook when the resolved
// IP address points at a private/loopback/etc. range while
// SUI_ALLOW_PRIVATE_SUB_URLS is not enabled.
var errBlockedExternalAddress = common.NewError("private url host is not allowed")

// allowPrivateExternalURLs reports whether SUI_ALLOW_PRIVATE_SUB_URLS opts the
// process out of private-address filtering for external subscription URLs.
func allowPrivateExternalURLs() bool {
	return os.Getenv("SUI_ALLOW_PRIVATE_SUB_URLS") == "true"
}

// getExternalHTTPClient returns a process-wide HTTP client that re-validates
// every dialed address against isBlockedExternalAddr. Re-validating at dial
// time prevents DNS-rebinding attacks where validateExternalURL sees a public
// address but the subsequent connection is steered to a private one.
func getExternalHTTPClient() *http.Client {
	externalHTTPClientOnce.Do(func() {
		externalHTTPClient = newExternalHTTPClient()
	})
	return externalHTTPClient
}

func newExternalHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		// These URLs are untrusted and require target-IP validation at the
		// actual dial boundary. An environment proxy would move DNS resolution
		// behind the proxy and bypass that rebinding defense.
		Proxy:                 nil,
		DialContext:           validatedExternalDial(dialer.DialContext),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
	}
	return &http.Client{
		Timeout:       15 * time.Second,
		Transport:     transport,
		CheckRedirect: externalRedirectPolicy,
	}
}

// Validate and pin the destination before handing it to either the host dialer
// or an explicitly selected runtime outbound. A detour cannot bypass the
// subscription owner's private-address and DNS-rebinding boundary.
func validatedExternalDial(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowPrivateExternalURLs() {
			return dial(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			if isBlockedExternalAddr(addr) {
				return nil, errBlockedExternalAddress
			}
			return dial(ctx, network, net.JoinHostPort(addr.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			addr, ok := netip.AddrFromSlice(ip.IP)
			if !ok || isBlockedExternalAddr(addr) {
				lastErr = errBlockedExternalAddress
				continue
			}
			conn, err := dial(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = errBlockedExternalAddress
		}
		return nil, lastErr
	}
}

func externalRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxExternalRedirects {
		return common.NewError("too many redirects")
	}
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	req.Header.Del("Referer")
	// Source-owned headers are private to the original request, including
	// custom token headers unknown to net/http's standard sensitive list.
	if names, ok := req.Context().Value(sourceHeaderNamesKey{}).([]string); ok {
		for _, name := range names {
			req.Header.Del(name)
		}
	}
	if req.URL == nil {
		return common.NewError("missing redirect url")
	}
	if len(via) > 0 && via[len(via)-1].URL != nil && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" {
		return common.NewError("subscription redirect downgrades https to http")
	}
	return ValidateURL(req.URL.String())
}

func Fetch(rawURL string) (string, error) {
	return FetchWithUserAgentContext(context.Background(), rawURL, "")
}

func FetchWithUserAgent(rawURL string, userAgent string) (string, error) {
	return FetchWithUserAgentContext(context.Background(), rawURL, userAgent)
}

func FetchContext(ctx context.Context, rawURL string) (string, error) {
	return FetchWithUserAgentContext(ctx, rawURL, "")
}

func FetchWithUserAgentContext(ctx context.Context, rawURL string, userAgent string) (string, error) {
	return FetchWithHeadersContext(ctx, rawURL, userAgent, nil)
}

func FetchWithHeadersContext(ctx context.Context, rawURL string, userAgent string, headers http.Header) (string, error) {
	return fetchWithClientContext(ctx, rawURL, userAgent, headers, getExternalHTTPClient())
}

// FetchWithOutboundContext borrows only the explicit outbound's dial authority.
// Its proxy, TLS shortcuts, cookie jar and redirect policy never replace the
// subscription owner's validation. The caller retains ownership of outbound.
func FetchWithOutboundContext(ctx context.Context, rawURL, userAgent string, headers http.Header, outbound *http.Client) (string, error) {
	if outbound == nil {
		return "", errors.New("SUBSCRIPTION_DOWNLOAD_OUTBOUND_UNAVAILABLE")
	}
	transport, ok := outbound.Transport.(*http.Transport)
	if !ok || transport == nil || transport.DialContext == nil {
		return "", errors.New("SUBSCRIPTION_DOWNLOAD_OUTBOUND_UNAVAILABLE")
	}
	client := newExternalHTTPClient()
	client.Transport.(*http.Transport).DialContext = validatedExternalDial(transport.DialContext)
	defer client.CloseIdleConnections()
	return fetchWithClientContext(ctx, rawURL, userAgent, headers, client)
}

func fetchWithClientContext(ctx context.Context, rawURL string, userAgent string, headers http.Header, client *http.Client) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ValidateFetchHeaders(headers); err != nil {
		return "", err
	}
	if len(headers) != 0 {
		normalized := make(http.Header, len(headers))
		names := make([]string, 0, len(headers))
		for name, values := range headers {
			key := http.CanonicalHeaderKey(name)
			names = append(names, key)
			normalized[key] = append([]string(nil), values...)
		}
		headers = normalized
		ctx = context.WithValue(ctx, sourceHeaderNamesKey{}, names)
	}
	if err := ValidateURLContext(ctx, rawURL); err != nil {
		logger.Warning("sub: invalid external URL:", err)
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if len(headers) != 0 {
		req.Header = headers.Clone()
	}
	if strings.TrimSpace(userAgent) != "" {
		req.Header.Set("User-Agent", strings.TrimSpace(userAgent))
	}
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errBlockedExternalAddress) {
			logger.Warning("sub: external URL resolves to blocked address:", err)
			return "", err
		}
		logger.Warning("sub: Error making HTTP request:", err)
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", common.NewErrorf("unexpected status code: %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxExternalSubBytes+1))
	if err != nil {
		logger.Warning("sub: Error reading response body:", err)
		return "", err
	}
	if len(body) > maxExternalSubBytes {
		return "", common.NewError("response is too large")
	}

	data := uricodec.DecodeOrOriginal(string(body))
	return data, nil
}

func FetchOutbounds(url string) ([]map[string]interface{}, error) {
	if len(url) == 0 {
		return nil, common.NewError("no url")
	}

	data, err := Fetch(url)
	if err != nil {
		return nil, err
	}
	return subparser.ParseExternalOutbounds(data, suburi.Parse)
}

func ValidateURL(rawURL string) error {
	return ValidateURLContext(context.Background(), rawURL)
}

func ValidateURLContext(ctx context.Context, rawURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return common.NewError("unsupported url scheme")
	}
	host := parsed.Hostname()
	if host == "" {
		return common.NewError("missing url host")
	}
	if strings.EqualFold(host, "localhost") {
		return common.NewError("localhost url is not allowed")
	}
	if allowPrivateExternalURLs() {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if isBlockedExternalAddr(addr) {
			return common.NewError("private url host is not allowed")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return common.NewError("url host did not resolve")
	}
	for _, ipAddr := range addrs {
		addr, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok || isBlockedExternalAddr(addr) {
			return common.NewError("private url host is not allowed")
		}
	}
	return nil
}

func isBlockedExternalAddr(addr netip.Addr) bool {
	return ssrf.IsBlockedAddr(addr)
}
