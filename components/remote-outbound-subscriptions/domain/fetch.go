//go:build !minimal

package remote

import (
	"context"
	feature "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
	subparser "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"strings"
)

type FetchOptions = feature.FetchOptions
type fetchCandidate = feature.FetchCandidate

const MaxFetchedOutbounds = feature.MaxFetchedOutbounds

var fetchSubscriptionData = subexternal.Fetch
var fetchSubscriptionDataWithUserAgent = subexternal.FetchWithUserAgent
var fetchSubscriptionDataContext = subexternal.FetchContext
var fetchSubscriptionDataWithUserAgentContext = subexternal.FetchWithUserAgentContext
var ValidateSourceUserAgent = feature.ValidateSourceUserAgent
var mergeFetchedSubscriptions = feature.MergeFetchedSubscriptions
var sanitizeFetchErrorText = feature.SanitizeFetchErrorText

func ValidateSubscriptionURL(rawURL string) error {
	if rawURL == "" {
		return common.NewError("no url")
	}
	if err := subexternal.ValidateURL(rawURL); err != nil {
		return common.NewError("invalid or disallowed subscription url")
	}
	return nil
}
func sourceFetchOptions(options FetchOptions, legacy bool) FetchOptions {
	if options.Parser == nil {
		options.Parser = subparser.SourceParser{}
	}
	if options.Transport == nil {
		options.Transport = backendFetchTransport{legacy}
	}
	return options
}

type backendFetchTransport struct{ legacy bool }

func (backendFetchTransport) ValidateURL(ctx context.Context, rawURL string) error {
	return subexternal.ValidateURLContext(ctx, rawURL)
}
func (b backendFetchTransport) Fetch(ctx context.Context, r feature.FetchRequest) (string, error) {
	if r.OutboundClient != nil {
		return subexternal.FetchWithOutboundContext(ctx, r.URL, r.UserAgent, r.Headers, r.OutboundClient)
	}
	if len(r.Headers) != 0 {
		return subexternal.FetchWithHeadersContext(ctx, r.URL, r.UserAgent, r.Headers)
	}
	if b.legacy {
		if strings.TrimSpace(r.UserAgent) == "" {
			return fetchSubscriptionData(r.URL)
		}
		return fetchSubscriptionDataWithUserAgent(r.URL, r.UserAgent)
	}
	if strings.TrimSpace(r.UserAgent) == "" {
		return fetchSubscriptionDataContext(ctx, r.URL)
	}
	return fetchSubscriptionDataWithUserAgentContext(ctx, r.URL, r.UserAgent)
}
func FetchSubscription(rawURL string) (*FetchedSubscription, error) {
	return FetchSubscriptionWithOptions(rawURL, FetchOptions{})
}
func FetchSubscriptionWithOptions(rawURL string, options FetchOptions) (*FetchedSubscription, error) {
	return feature.FetchSubscriptionWithOptionsContext(context.Background(), rawURL, sourceFetchOptions(options, len(options.Headers) == 0 && options.OutboundClient == nil))
}
func FetchSubscriptionWithOptionsContext(ctx context.Context, rawURL string, options FetchOptions) (*FetchedSubscription, error) {
	return feature.FetchSubscriptionWithOptionsContext(ctx, rawURL, sourceFetchOptions(options, false))
}
func ParseFetchedSubscription(data string) (*FetchedSubscription, error) {
	return ParseFetchedSubscriptionWithOptions(data, FetchOptions{})
}
func ParseFetchedSubscriptionWithOptions(data string, options FetchOptions) (*FetchedSubscription, error) {
	return parseFetchedSubscriptionWithOptionsContext(context.Background(), data, options)
}
func parseFetchedSubscriptionWithOptionsContext(ctx context.Context, data string, options FetchOptions) (*FetchedSubscription, error) {
	return feature.ParseFetchedSubscriptionWithOptionsContext(ctx, data, sourceFetchOptions(options, false))
}
func subscriptionFetchCandidates(rawURL string) ([]fetchCandidate, error) {
	return feature.SubscriptionFetchCandidates(context.Background(), rawURL, backendFetchTransport{})
}

const maxFetchErrorBytes = feature.MaxFetchErrorBytes
