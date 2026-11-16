package external

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/logger"
)

type diagnosticRoundTripFunc func(*http.Request) (*http.Response, error)

func (f diagnosticRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type diagnosticBody struct {
	failure error
	closed  bool
}

func (b *diagnosticBody) Read([]byte) (int, error) { return 0, b.failure }
func (b *diagnosticBody) Close() error             { b.closed = true; return nil }

func TestFetchTransportLogsNeverContainCredentialBearingSourceLocations(t *testing.T) {
	t.Setenv("SUI_ALLOW_PRIVATE_SUB_URLS", "false")
	for _, stage := range []string{"validation", "request", "blocked dial", "body"} {
		t.Run(stage, func(t *testing.T) {
			canary := "fixture-source-secret-" + strings.ReplaceAll(stage, " ", "-")
			location := "https://93.184.216.34/private-" + canary + "?key=" + canary
			failure := errors.New("source transport failed for " + location)
			body := &diagnosticBody{failure: failure}
			client := &http.Client{Transport: diagnosticRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch stage {
				case "validation":
					t.Error("invalid source reached HTTP client")
					return nil, io.ErrUnexpectedEOF
				case "request":
					return nil, failure
				case "blocked dial":
					return nil, errBlockedExternalAddress
				default:
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request}, nil
				}
			})}
			if stage == "validation" {
				location = "https://93.184.216.34/private-" + canary + "%zz"
			}
			_, err := fetchWithClientContext(context.Background(), location, "fixture/1.0", nil, client)
			if err == nil {
				t.Fatal("failed source transport reported success")
			}
			if stage == "request" || stage == "body" {
				if !errors.Is(err, failure) {
					t.Fatal("transport error identity lost")
				}
			}
			if stage == "blocked dial" && !errors.Is(err, errBlockedExternalAddress) {
				t.Fatal("blocked address identity lost")
			}
			if stage == "body" && !body.closed {
				t.Fatal("failed response body was not closed")
			}
			entries := logger.Entries(100, "WARN", "panel", "sub:")
			if len(entries) == 0 {
				t.Fatal("failed transport omitted operation evidence")
			}
			for _, entry := range entries {
				if strings.Contains(entry.Message, canary) {
					t.Fatal("transport log leaked source URL path/query credentials")
				}
			}
		})
	}
}
