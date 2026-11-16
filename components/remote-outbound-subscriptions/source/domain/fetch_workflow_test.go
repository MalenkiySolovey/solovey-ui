//go:build !minimal

package remote

import (
	"context"
	"errors"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/parseport"
	"net/url"
	"strings"
	"testing"
)

type fetchTransportFixture struct {
	validate func(context.Context, string) error
	fetch    func(context.Context, FetchRequest) (string, error)
}

func (f fetchTransportFixture) ValidateURL(ctx context.Context, raw string) error {
	return f.validate(ctx, raw)
}
func (f fetchTransportFixture) Fetch(ctx context.Context, request FetchRequest) (string, error) {
	return f.fetch(ctx, request)
}

type parserFixture func(context.Context, []byte, parseport.Options) (parseport.Result, error)

func (f parserFixture) Parse(ctx context.Context, data []byte, options parseport.Options) (parseport.Result, error) {
	return f(ctx, data, options)
}
func representationParser(ctx context.Context, data []byte, options parseport.Options) (parseport.Result, error) {
	if err := ctx.Err(); err != nil {
		return parseport.Result{}, err
	}
	if string(data) != options.Format {
		return parseport.Result{}, errors.New("unsupported fixture representation")
	}
	return parseport.Result{Outbounds: []map[string]any{{"type": "vless", "tag": options.Format, "server": options.Format + ".example.org", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001"}}}, nil
}
func TestLegacyFetchMergesRepresentationsAndExplicitIdentityFetchesOnce(t *testing.T) {
	var requests []FetchRequest
	transport := fetchTransportFixture{validate: func(context.Context, string) error { return nil }, fetch: func(_ context.Context, request FetchRequest) (string, error) {
		requests = append(requests, request)
		parsed, _ := url.Parse(request.URL)
		switch parsed.Query().Get("format") {
		case "xray", "v2ray":
			return canonical.FormatXray, nil
		case "clash":
			return canonical.FormatClash, nil
		case "json":
			return canonical.FormatSingBox, nil
		default:
			return canonical.FormatURI, nil
		}
	}}
	options := FetchOptions{Transport: transport, Parser: parserFixture(representationParser)}
	fetched, err := FetchSubscriptionWithOptionsContext(context.Background(), "https://source.example.org/path?key=fixture", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 6 || len(fetched.Attempts) != 6 || len(fetched.Outbounds) != 4 || !fetched.Complete() {
		t.Fatalf("legacy merge changed: requests=%d outbounds=%d attempts=%v", len(requests), len(fetched.Outbounds), fetched.Attempts)
	}
	for _, request := range requests {
		if !strings.Contains(request.URL, "key=fixture") {
			t.Fatal("provider parameters lost")
		}
	}
	requests = nil
	options.UserAgent = "CustomClient/1"
	fetched, err = FetchSubscriptionWithOptionsContext(context.Background(), "https://source.example.org/original?key=fixture", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].URL != "https://source.example.org/original?key=fixture" || requests[0].UserAgent != options.UserAgent || len(fetched.Outbounds) != 1 {
		t.Fatal("explicit client identity did not use exactly the original resource")
	}
}
func TestFetchRejectsMissingAuthorityAndPropagatesCancellation(t *testing.T) {
	called := 0
	transport := fetchTransportFixture{validate: func(context.Context, string) error { return errors.New("forbidden") }, fetch: func(context.Context, FetchRequest) (string, error) { called++; return canonical.FormatURI, nil }}
	if _, err := FetchSubscriptionWithOptionsContext(context.Background(), "https://source.example.org", FetchOptions{}); err == nil {
		t.Fatal("missing transport accepted")
	}
	if _, err := FetchSubscriptionWithOptionsContext(context.Background(), "https://source.example.org", FetchOptions{Transport: transport}); err == nil || called != 0 {
		t.Fatal("disallowed URL reached transport")
	}
	if _, err := ParseFetchedSubscription(canonical.FormatURI); err == nil {
		t.Fatal("missing parser accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport.validate = func(context.Context, string) error { return nil }
	transport.fetch = func(ctx context.Context, _ FetchRequest) (string, error) { called++; cancel(); return "", ctx.Err() }
	_, err := FetchSubscriptionWithOptionsContext(ctx, "https://source.example.org", FetchOptions{Transport: transport, Parser: parserFixture(representationParser)})
	if !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatalf("canceled fetch continued: calls=%d err=%v", called, err)
	}
}
func TestPartialFetchCannotAuthorizePruningAndErrorsAreRedacted(t *testing.T) {
	transport := fetchTransportFixture{validate: func(context.Context, string) error { return nil }, fetch: func(_ context.Context, request FetchRequest) (string, error) {
		if strings.Contains(request.URL, "format=json") {
			return canonical.FormatSingBox, nil
		}
		return "", errors.New("GET https://user:password@source.example.org/private?token=secret-token failed password=hidden-value")
	}}
	fetched, err := FetchSubscriptionWithOptionsContext(context.Background(), "https://source.example.org", FetchOptions{Transport: transport, Parser: parserFixture(representationParser)})
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Complete() || len(fetched.Outbounds) != 1 {
		t.Fatal("partial representation authorized a complete snapshot")
	}
	for _, attempt := range fetched.Attempts {
		for _, secret := range []string{"user:password", "private?token", "secret-token", "hidden-value"} {
			if strings.Contains(attempt.Error, secret) {
				t.Fatalf("secret leaked: %s", secret)
			}
		}
	}
}
func TestFetchedEndpointLimitAndMalformedInputFailClosed(t *testing.T) {
	parser := parserFixture(func(_ context.Context, _ []byte, options parseport.Options) (parseport.Result, error) {
		if options.Format != canonical.FormatSingBox {
			return parseport.Result{}, errors.New("malformed JSON/YAML fixture")
		}
		outbounds := make([]map[string]any, MaxFetchedOutbounds+1)
		for i := range outbounds {
			outbounds[i] = map[string]any{"type": "vless", "server": "node.example.org", "server_port": i + 1, "tag": PortString(i + 1)}
		}
		return parseport.Result{Outbounds: outbounds}, nil
	})
	if _, err := ParseFetchedSubscriptionWithOptions("oversized fixture", FetchOptions{Parser: parser}); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("endpoint bound not enforced: %v", err)
	}
	parser = func(context.Context, []byte, parseport.Options) (parseport.Result, error) {
		return parseport.Result{}, errors.New("invalid token=never-expose-this " + strings.Repeat("x", 8192))
	}
	_, err := ParseFetchedSubscriptionWithOptions("malformed", FetchOptions{Parser: parser})
	if err == nil || strings.Contains(err.Error(), "never-expose-this") || len(err.Error()) > MaxFetchErrorBytes+128 {
		t.Fatal("parser failure is absent, unbounded or exposes credentials")
	}
}

func TestFetchErrorURLRedactionHandlesUserInfoAndMalformedURLs(t *testing.T) {
	for _, raw := range []string{"https://user:password@source.example.org/private-token?key=credential", "https://[REDACTED]@source.example.org/private-token?key=credential", "https://source.example.org/%invalid/private-token?key=credential", "HTTPS://source.example.org/private-token?key=credential"} {
		value := SanitizeFetchErrorText("fetch failed: " + raw)
		for _, secret := range []string{"user:", "password", "private-token", "credential"} {
			if strings.Contains(value, secret) {
				t.Fatalf("URL diagnostic leaked %q", secret)
			}
		}
	}
}

func TestCanceledSourceValidationRetainsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := fetchTransportFixture{validate: func(context.Context, string) error { cancel(); return context.Canceled }, fetch: func(context.Context, FetchRequest) (string, error) {
		t.Fatal("canceled validation fetched")
		return "", nil
	}}
	_, err := FetchSubscriptionWithOptionsContext(ctx, "https://source.example.org", FetchOptions{Transport: transport, Parser: parserFixture(representationParser)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation disguised as invalid source: %v", err)
	}
}
