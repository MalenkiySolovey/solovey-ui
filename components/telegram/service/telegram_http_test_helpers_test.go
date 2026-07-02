//go:build !minimal

package telegram_test

import (
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
)

type countingRoundTripper struct {
	count atomic.Int32
}

func (r *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.count.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}

func (r *countingRoundTripper) Count() int {
	return int(r.count.Load())
}

type statusRoundTripper struct {
	status int
}

func (r statusRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: r.status,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}

type telegramServerRoundTripper struct {
	base      *url.URL
	transport http.RoundTripper
}

func (r telegramServerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = r.base.Scheme
	cloned.URL.Host = r.base.Host
	return r.transport.RoundTrip(cloned)
}

type captureRoundTripper struct {
	req  *http.Request
	body []byte
}

func (r *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	r.body = body
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}
