package remote

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
	subparser "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

type FormatResult struct {
	Format    string                   `json:"format"`
	Outbounds []map[string]interface{} `json:"-"`
	Error     string                   `json:"error,omitempty"`
}

type FetchAttempt struct {
	Variant string   `json:"variant"`
	Formats []string `json:"formats,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type FetchedSubscription struct {
	Outbounds []map[string]interface{}
	Snapshot  canonical.Snapshot
	Formats   []FormatResult
	Attempts  []FetchAttempt
}

type CollectionSnapshot struct {
	Formats  []FormatResult `json:"formats,omitempty"`
	Attempts []FetchAttempt `json:"attempts,omitempty"`
}

type FetchOptions struct {
	GroupAdaptation  string
	ConversionPolicy subconversion.Policy
}

type fetchCandidate struct {
	Variant   string
	URL       string
	UserAgent string
}

var fetchSubscriptionData = subexternal.Fetch
var fetchSubscriptionDataWithUserAgent = subexternal.FetchWithUserAgent

var (
	fetchErrorURLPattern    = regexp.MustCompile(`https?://[^\s"'<>]+`)
	fetchErrorSecretPattern = regexp.MustCompile(`(?i)((?:token|secret|password|passphrase|api[_-]?key)\s*[=:]\s*)[^\s,;"']+`)
)

func ValidateSubscriptionURL(rawURL string) error {
	if rawURL == "" {
		return common.NewError("no url")
	}
	return subexternal.ValidateURL(rawURL)
}

func FetchOutbounds(rawURL string) ([]map[string]interface{}, error) {
	fetched, err := FetchSubscription(rawURL)
	if err != nil {
		return nil, err
	}
	return fetched.Outbounds, nil
}

func FetchSubscription(rawURL string) (*FetchedSubscription, error) {
	return FetchSubscriptionWithOptions(rawURL, FetchOptions{})
}

func FetchSubscriptionWithOptions(rawURL string, options FetchOptions) (*FetchedSubscription, error) {
	if rawURL == "" {
		return nil, common.NewError("no url")
	}
	candidates, err := subscriptionFetchCandidates(rawURL)
	if err != nil {
		return nil, err
	}
	var attempts []FetchAttempt
	var fetchedParts []*FetchedSubscription
	seenFormats := map[string]struct{}{}
	for _, candidate := range candidates {
		attempt := FetchAttempt{Variant: candidate.Variant}
		data, err := fetchCandidateData(candidate)
		if err != nil {
			attempt.Error = sanitizeFetchError(err)
			attempts = append(attempts, attempt)
			continue
		}
		fetched, err := ParseFetchedSubscriptionWithOptions(data, options)
		if err != nil {
			attempt.Error = err.Error()
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
	return mergeFetchedSubscriptions(attempts, fetchedParts...), nil
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

func ParseFetchedOutbounds(data string) ([]map[string]interface{}, error) {
	fetched, err := ParseFetchedSubscription(data)
	if err != nil {
		return nil, err
	}
	return fetched.Outbounds, nil
}

func ParseFetchedSubscription(data string) (*FetchedSubscription, error) {
	return ParseFetchedSubscriptionWithOptions(data, FetchOptions{})
}

func ParseFetchedSubscriptionWithOptions(data string, options FetchOptions) (*FetchedSubscription, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("no result")
	}
	parseOptions := subparser.ParseOptions{
		GroupAdaptation:  options.GroupAdaptation,
		ConversionPolicy: fetchConversionPolicy(options),
	}
	formats := []FormatResult{
		parseFetchedFormat(canonical.FormatXray, data, func(raw string) ([]map[string]interface{}, error) {
			return subparser.ParseXrayOutboundsWithOptions(raw, parseOptions)
		}),
		parseFetchedFormat(canonical.FormatClash, data, func(raw string) ([]map[string]interface{}, error) {
			return subparser.ParseClashOutboundsWithOptions(raw, parseOptions)
		}),
		parseFetchedFormat(canonical.FormatSingBox, data, subparser.ParseSingBoxOutbounds),
		parseFetchedFormat(canonical.FormatURI, data, func(raw string) ([]map[string]interface{}, error) {
			return subparser.ParseExternalLinkOutbounds(raw, suburi.Parse)
		}),
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
	return &FetchedSubscription{Outbounds: outbounds, Snapshot: snapshot, Formats: formats}, nil
}

func subscriptionFetchCandidates(rawURL string) ([]fetchCandidate, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := subexternal.ValidateURL(rawURL); err != nil {
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
	candidates := make([]fetchCandidate, 0, len(variants))
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
		candidates = append(candidates, fetchCandidate{
			Variant:   variant.name,
			URL:       candidateURL,
			UserAgent: variant.userAgent,
		})
	}
	return candidates, nil
}

func fetchCandidateData(candidate fetchCandidate) (string, error) {
	if strings.TrimSpace(candidate.UserAgent) == "" {
		return fetchSubscriptionData(candidate.URL)
	}
	return fetchSubscriptionDataWithUserAgent(candidate.URL, candidate.UserAgent)
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

func mergeFetchedSubscriptions(attempts []FetchAttempt, fetchedParts ...*FetchedSubscription) *FetchedSubscription {
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
		result.Error = err.Error()
		return result
	}
	result.Outbounds = outbounds
	return result
}

func firstFormatError(results []FormatResult) error {
	for _, result := range results {
		if result.Error != "" {
			return common.NewError("unsupported subscription format: ", result.Error)
		}
	}
	return common.NewError("unsupported subscription format")
}

func firstFetchAttemptError(attempts []FetchAttempt) error {
	for _, attempt := range attempts {
		if attempt.Error != "" {
			return common.NewError("unable to fetch subscription format ", attempt.Variant, ": ", attempt.Error)
		}
	}
	return common.NewError("unable to fetch subscription")
}

func sanitizeFetchError(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeFetchErrorText(err.Error())
}

func sanitizeFetchErrorText(value string) string {
	value = redact.String(value)
	value = fetchErrorSecretPattern.ReplaceAllString(value, `${1}[redacted]`)
	return fetchErrorURLPattern.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return raw
		}
		return parsed.Scheme + "://" + parsed.Host + "/[redacted]"
	})
}
