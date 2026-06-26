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
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if allowPrivateExternalURLs() {
					return dialer.DialContext(ctx, network, addr)
				}
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if addr, err := netip.ParseAddr(host); err == nil {
					if isBlockedExternalAddr(addr) {
						return nil, errBlockedExternalAddress
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
					if !ok || isBlockedExternalAddr(addr) {
						lastErr = errBlockedExternalAddress
						continue
					}
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
					if err == nil {
						return conn, nil
					}
					lastErr = err
				}
				if lastErr == nil {
					lastErr = errBlockedExternalAddress
				}
				return nil, lastErr
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          10,
			MaxIdleConnsPerHost:   2,
		}
		externalHTTPClient = &http.Client{
			Timeout:       15 * time.Second,
			Transport:     transport,
			CheckRedirect: externalRedirectPolicy,
		}
	})
	return externalHTTPClient
}

func externalRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxExternalRedirects {
		return common.NewError("too many redirects")
	}
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	req.Header.Del("Referer")
	if req.URL == nil {
		return common.NewError("missing redirect url")
	}
	if len(via) > 0 && via[len(via)-1].URL != nil && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" {
		return common.NewError("subscription redirect downgrades https to http")
	}
	return ValidateURL(req.URL.String())
}

func Fetch(rawURL string) (string, error) {
	return FetchWithUserAgent(rawURL, "")
}

func FetchWithUserAgent(rawURL string, userAgent string) (string, error) {
	if err := ValidateURL(rawURL); err != nil {
		logger.Warning("sub: invalid external URL:", err)
		return "", err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(userAgent) != "" {
		req.Header.Set("User-Agent", strings.TrimSpace(userAgent))
	}
	response, err := getExternalHTTPClient().Do(req)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
