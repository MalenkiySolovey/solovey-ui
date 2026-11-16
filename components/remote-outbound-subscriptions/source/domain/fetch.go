//go:build !minimal

package remote

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/MalenkiySolovey/solovey-host/redact"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-host/subscriptions/parseport"
)

type FetchOptions struct {
	Transport        FetchTransport
	Parser           parseport.Parser
	UserAgent        string
	Headers          http.Header
	OutboundClient   *http.Client
	GroupAdaptation  string
	ConversionPolicy subconversion.Policy
}

type FetchCandidate struct {
	Variant   string
	URL       string
	UserAgent string
}

// FetchTransport keeps the existing Backend URL, redirect, DNS and outbound
// dial policy as the only transport authority. A missing adapter fails closed.
type FetchRequest struct {
	URL, UserAgent string
	Headers        http.Header
	OutboundClient *http.Client
}
type FetchTransport interface {
	ValidateURL(context.Context, string) error
	Fetch(context.Context, FetchRequest) (string, error)
}

const MaxFetchErrorBytes = 4 << 10
const MaxFetchedOutbounds = 4096

var (
	fetchErrorURLPattern    = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	fetchErrorSecretPattern = regexp.MustCompile(`(?i)((?:token|secret|password|passphrase|api[_-]?key)\s*[=:]\s*)[^\s,;"']+`)
)

func ValidateSourceUserAgent(value string) error {
	if len(value) > 4096 || strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7F }) {
		return sourceError("SUBSCRIPTION_SOURCE_USER_AGENT_INVALID")
	}
	return nil
}

func FetchSubscriptionWithOptionsContext(ctx context.Context, rawURL string, options FetchOptions) (*FetchedSubscription, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Transport == nil {
		return nil, sourceError("subscription source transport is unavailable")
	}
	return fetchSubscriptionWithOptions(ctx, rawURL, options, func(candidate FetchCandidate) (string, error) {
		return options.Transport.Fetch(ctx, FetchRequest{URL: candidate.URL, UserAgent: candidate.UserAgent, Headers: options.Headers, OutboundClient: options.OutboundClient})
	})
}

func fetchSubscriptionWithOptions(ctx context.Context, rawURL string, options FetchOptions, fetch func(FetchCandidate) (string, error)) (*FetchedSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateSourceUserAgent(options.UserAgent); err != nil {
		return nil, err
	}
	if rawURL == "" {
		return nil, sourceError("no url")
	}
	candidates, err := SubscriptionFetchCandidates(ctx, rawURL, options.Transport)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, sourceError("invalid or disallowed subscription url")
	}
	if userAgent := strings.TrimSpace(options.UserAgent); userAgent != "" {
		// An explicit client identity requests the original source once. The
		// automatic multi-format identities remain the default when omitted.
		candidates = []FetchCandidate{{Variant: "custom-user-agent", URL: rawURL, UserAgent: userAgent}}
	}
	var attempts []FetchAttempt
	var fetchedParts []*FetchedSubscription
	seenFormats := map[string]struct{}{}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt := FetchAttempt{Variant: candidate.Variant}
		data, err := fetch(candidate)
		if err != nil {
			attempt.Error = sanitizeFetchError(err)
			attempts = append(attempts, attempt)
			continue
		}
		fetched, err := ParseFetchedSubscriptionWithOptionsContext(ctx, data, options)
		if err != nil {
			attempt.Error = sanitizeFetchError(err)
			attempts = append(attempts, attempt)
			continue
		}
		attempt.Formats = successfulFormats(fetched.Formats)
		attempts = append(attempts, attempt)
		if !hasNewFormat(attempt.Formats, seenFormats) {
			continue
		}
		for _, format := range attempt.Formats {
			if strings.TrimSpace(format) != "" {
				seenFormats[format] = struct{}{}
			}
		}
		fetchedParts = append(fetchedParts, fetched)
	}
	if len(fetchedParts) == 0 {
		return nil, firstFetchAttemptError(attempts)
	}
	merged := MergeFetchedSubscriptions(attempts, fetchedParts...)
	if len(merged.Outbounds) > MaxFetchedOutbounds {
		return nil, sourceError("remote subscription contains too many outbounds")
	}
	return merged, nil
}

func hasNewFormat(formats []string, seen map[string]struct{}) bool {
	for _, format := range formats {
		format = strings.TrimSpace(format)
		if format == "" {
			continue
		}
		if _, ok := seen[format]; !ok {
			return true
		}
	}
	return false
}

func ParseFetchedSubscription(data string) (*FetchedSubscription, error) {
	return ParseFetchedSubscriptionWithOptions(data, FetchOptions{})
}

func ParseFetchedSubscriptionWithOptions(data string, options FetchOptions) (*FetchedSubscription, error) {
	return ParseFetchedSubscriptionWithOptionsContext(context.Background(), data, options)
}

func ParseFetchedSubscriptionWithOptionsContext(ctx context.Context, data string, options FetchOptions) (*FetchedSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, sourceError("no result")
	}
	parser := options.Parser
	if parser == nil {
		return nil, sourceError("subscription source parser is unavailable")
	}
	parse := func(format string) FormatResult {
		return parseFetchedFormat(format, data, func(raw string) ([]map[string]interface{}, error) {
			result, err := parser.Parse(ctx, []byte(raw), parseport.Options{Format: format, GroupAdaptation: options.GroupAdaptation, ConversionPolicy: fetchConversionPolicy(options)})
			return result.Outbounds, err
		})
	}
	formats := []FormatResult{parse(canonical.FormatXray), parse(canonical.FormatClash), parse(canonical.FormatSingBox), parse(canonical.FormatURI)}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshots := make([]canonical.Snapshot, 0, len(formats))
	for _, result := range formats {
		if len(result.Outbounds) == 0 {
			continue
		}
		snapshots = append(snapshots, canonical.ObserveOutbounds(result.Format, result.Outbounds))
	}
	snapshot := canonical.MergeSnapshots(snapshots...)
	outbounds := canonical.SnapshotOutbounds(snapshot)
	if len(outbounds) == 0 {
		return nil, firstFormatError(formats)
	}
	if len(outbounds) > MaxFetchedOutbounds {
		return nil, sourceError("remote subscription contains too many outbounds")
	}
	return &FetchedSubscription{Outbounds: outbounds, Snapshot: snapshot, Formats: formats}, nil
}

func SubscriptionFetchCandidates(ctx context.Context, rawURL string, transport FetchTransport) ([]FetchCandidate, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, sourceError("subscription source transport is unavailable")
	}
	if err := transport.ValidateURL(ctx, rawURL); err != nil {
		return nil, err
	}
	variants := []struct {
		name      string
		format    string
		userAgent string
	}{
		{name: canonical.FormatXray, format: "xray", userAgent: "v2rayNG/1.10.14"},
		{name: "v2ray-json", format: "v2ray", userAgent: "v2rayNG/1.10.14"},
		{name: canonical.FormatClash, format: "clash", userAgent: "ClashMetaForAndroid/2.11.16"},
		{name: canonical.FormatSingBox, format: "json", userAgent: "sing-box/1.12.0"},
		{name: canonical.FormatURI, format: "uri"},
		{name: "original"},
	}
	candidates := make([]FetchCandidate, 0, len(variants))
	seen := map[string]struct{}{}
	for _, variant := range variants {
		next := *parsed
		if variant.format != "" {
			query := next.Query()
			query.Set("format", variant.format)
			next.RawQuery = query.Encode()
		}
		candidateURL := next.String()
		seenKey := candidateURL + "\x00" + variant.userAgent
		if _, ok := seen[seenKey]; ok {
			continue
		}
		seen[seenKey] = struct{}{}
		candidates = append(candidates, FetchCandidate{
			Variant:   variant.name,
			URL:       candidateURL,
			UserAgent: variant.userAgent,
		})
	}
	return candidates, nil
}

func fetchConversionPolicy(options FetchOptions) subconversion.Policy {
	if options.ConversionPolicy.Outbound != nil ||
		options.ConversionPolicy.Client.SingBox != nil ||
		options.ConversionPolicy.Client.Xray != nil ||
		options.ConversionPolicy.Client.Mihomo != nil {
		return options.ConversionPolicy
	}
	return subconversion.ParsePolicy("", options.GroupAdaptation)
}

func MergeFetchedSubscriptions(attempts []FetchAttempt, fetchedParts ...*FetchedSubscription) *FetchedSubscription {
	snapshots := make([]canonical.Snapshot, 0, len(fetchedParts))
	formats := make([]FormatResult, 0)
	for _, fetched := range fetchedParts {
		if fetched == nil {
			continue
		}
		snapshots = append(snapshots, fetched.Snapshot)
		formats = append(formats, fetched.Formats...)
	}
	snapshot := canonical.MergeSnapshots(snapshots...)
	return &FetchedSubscription{
		Outbounds: canonical.SnapshotOutbounds(snapshot),
		Snapshot:  snapshot,
		Formats:   formats,
		Attempts:  attempts,
	}
}

func successfulFormats(results []FormatResult) []string {
	formats := make([]string, 0, len(results))
	for _, result := range results {
		if len(result.Outbounds) == 0 {
			continue
		}
		formats = append(formats, result.Format)
	}
	return formats
}

func parseFetchedFormat(format string, data string, parse func(string) ([]map[string]interface{}, error)) FormatResult {
	result := FormatResult{Format: format}
	outbounds, err := parse(data)
	if err != nil {
		result.Error = sanitizeFetchError(err)
		return result
	}
	result.Outbounds = outbounds
	return result
}

func firstFormatError(results []FormatResult) error {
	for _, result := range results {
		if result.Error != "" {
			return sourceError("unsupported subscription format: ", result.Error)
		}
	}
	return sourceError("unsupported subscription format")
}

func firstFetchAttemptError(attempts []FetchAttempt) error {
	for _, attempt := range attempts {
		if attempt.Error != "" {
			return sourceError("unable to fetch subscription format ", attempt.Variant, ": ", attempt.Error)
		}
	}
	return sourceError("unable to fetch subscription")
}

func sanitizeFetchError(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeFetchErrorText(err.Error())
}

func SanitizeFetchErrorText(value string) string {
	// Remove URL paths before generic userinfo redaction inserts a marker that
	// net/url cannot parse as userinfo. Invalid URLs must also fail closed.
	value = fetchErrorURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "[redacted-url]"
		}
		return parsed.Scheme + "://" + parsed.Host + "/[redacted]"
	})
	value = redact.String(value)
	value = fetchErrorSecretPattern.ReplaceAllString(value, `${1}[redacted]`)
	return redact.StringLimit(value, MaxFetchErrorBytes)
}
