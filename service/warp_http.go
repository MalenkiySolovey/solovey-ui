package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"io"
	"net"
	"net/http"
	"time"
)

type WarpService struct{}

// warpAPIVersions lists Cloudflare WARP REST API versions in the order we
// will try them. The newer `v0a4005` endpoint is what current first-party
// clients (1.1.1.1 desktop / wgcf) speak; the older `v0a2158` endpoint
// occasionally still works and is kept as a fallback for hosts where the
// new endpoint refuses the connection.
var warpAPIVersions = []string{"v0a4005", "v0a2158"}

// warpUserAgent mimics a current 1.1.1.1 desktop client. Without this header
// Cloudflare regularly drops the TLS connection mid-stream (`EOF`) before
// returning a body.
const warpUserAgent = "1.1.1.1/6.81"

// warpClientVersion mirrors the matching CF-Client-Version a recent first
// party client sends.
const warpClientVersion = "a-6.81-3343"

// warpHTTPClient is the dedicated client used for Cloudflare WARP API
// calls. The Cloudflare endpoint is fussy about TLS minor versions and
// HTTP/2 multiplexing on slow uplinks, so we pin TLS 1.2+ and stay on
// HTTP/1.1.
var warpHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   false,
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 30 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// setWarpHeaders applies the headers a current first-party WARP client
// sends. Cloudflare uses these to distinguish trusted clients from generic
// HTTP clients; without them registration requests are met with `EOF`.
func setWarpHeaders(req *http.Request) {
	req.Header.Set("User-Agent", warpUserAgent)
	req.Header.Set("CF-Client-Version", warpClientVersion)
	req.Header.Set("Accept", "application/json; charset=UTF-8")
	req.Header.Set("Accept-Encoding", "identity")
	if req.Method != http.MethodGet && req.Method != http.MethodDelete {
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		}
	}
}
func setWarpAuthorizedHeaders(req *http.Request, accessToken string) {
	setWarpHeaders(req)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

// doWarpAttempt performs a single HTTP attempt with proper body cloning so
// retries can replay POSTs / PUTs.
func doWarpAttempt(req *http.Request, body []byte) (*http.Response, error) {
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return warpHTTPClient.Do(req)
}

// doWarpRequestVersions issues the same request against each WARP API
// version until one returns a 2xx response. The provided `mkRequest`
// callback rebuilds the request for a given version (the URL changes).
//
// Each version is retried up to 3 times to absorb transient TLS / network
// hiccups. The last error is preserved when all attempts fail.
func doWarpRequestVersions(mkRequest func(version string) (*http.Request, []byte, error)) (*http.Response, string, error) {
	const attemptsPerVersion = 3
	var lastErr error
	for _, version := range warpAPIVersions {
		for attempt := 1; attempt <= attemptsPerVersion; attempt++ {
			req, body, err := mkRequest(version)
			if err != nil {
				return nil, "", err
			}
			setWarpHeaders(req)
			resp, err := doWarpAttempt(req, body)
			if err == nil {
				if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
					return resp, version, nil
				}
				// 4xx / 5xx — no point retrying within the same version,
				// but the next version may behave differently.
				_ = resp.Body.Close()
				lastErr = common.NewErrorf("cloudflare warp %s status: %d", version, resp.StatusCode)
				logger.Warningf("warp request to %s returned %d, will try other versions", version, resp.StatusCode)
				break
			}
			lastErr = err
			logger.Warningf("warp request attempt %d/%d on %s failed: %v", attempt, attemptsPerVersion, version, err)
			// EOF / connection-reset are the most likely failure modes here;
			// a brief backoff helps Cloudflare recycle the trust window.
			if attempt < attemptsPerVersion {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("cloudflare warp: all attempts failed")
	}
	return nil, "", lastErr
}
func (s *WarpService) getWarpInfo(version, deviceId, accessToken string) ([]byte, error) {
	url := fmt.Sprintf("https://api.cloudflareclient.com/%s/reg/%s", version, deviceId)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setWarpAuthorizedHeaders(req, accessToken)
	resp, err := doWarpAttempt(req, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, common.NewErrorf("cloudflare warp status: %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
