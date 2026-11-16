//go:build !minimal

package remotesubservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	"strings"
)

func serviceError(values ...any) error {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return errors.New(strings.Join(parts, " "))
}
func (s *Service) warning(values ...any) {
	if s.LogWarning != nil {
		s.LogWarning(values...)
	}
}

type ProbeResult struct{ Error string }
type Effects interface {
	RecordChange(Store, string, string, any) error
	Invalidate(string, bool)
}
type SourceSecurity interface {
	PrepareDialerOverride(json.RawMessage) (json.RawMessage, error)
	PrepareSourceHealthCheck(json.RawMessage) (json.RawMessage, error)
	ParseSourceHealthCheck(json.RawMessage) (remotesub.SourceHealthCheck, error)
	ValidateSubscriptionURL(string) error
	ValidateSourceFilePath(string) error
	ApplySourceContent(context.Context, Store, *remotesub.RemoteOutboundSubscription, json.RawMessage, remotesub.FetchOptions) error
	ApplySourceHeaders(Store, *remotesub.RemoteOutboundSubscription, json.RawMessage) error
	ProjectSourceContent(*remotesub.RemoteOutboundSubscription) error
	OutboundAvailable(string, json.RawMessage) bool
}
type SourceFetcher interface {
	FetchSource(context.Context, Store, remotesub.RemoteOutboundSubscription, remotesub.FetchOptions) (*remotesub.FetchedSubscription, error)
}
type ConnectionChecker interface {
	CheckConnection(context.Context, uint, string) (*remotesub.CheckResult, error)
	CheckSubscription(context.Context, uint, string) ([]remotesub.CheckResult, error)
	CheckAll(context.Context, string) ([]remotesub.CheckResult, error)
}

// Store preserves Backend transaction and admitted-operation ownership.
type Store interface {
	remotesub.Store
	WithContext(context.Context) Store
	OperationContext(context.Context) context.Context
	Transaction(func(Store) error) error
	AcquireOperation(context.Context) (context.Context, func(), error)
	ValidateDownloadOutbound(uint, uint) error
	ListSubscriptionDetails(out *[]remotesub.RemoteOutboundSubscription) error
	SubscriptionSaveOrder(id uint) (int, error)
	SaveSubscription(value *remotesub.RemoteOutboundSubscription) error
	LoadSubscriptionDetails(id uint, out *remotesub.RemoteOutboundSubscription) error
	DeleteSubscriptionMemberships(subscriptionID uint) error
	DeleteSubscriptionConnections(subscriptionID uint) error
	DeleteSubscriptionGroups(subscriptionID uint) error
	DeleteSubscription(subscriptionID uint) error
	CountSubscription(id uint, out *int64) error
	GroupSaveOrder(id uint) (int, error)
	ListOrderedSubscriptions(out *[]remotesub.RemoteOutboundSubscription) error
	CountNamedGroup(subscriptionID uint, name string, out *int64) error
	MaxSubscriptionGroupOrder(subscriptionID uint, out *int) error
	LoadCollectedSubscription(id uint, out *remotesub.RemoteOutboundSubscription) error
	ListHealthSources(after uint, limit int, out *[]remotesub.RemoteOutboundSubscription) error
	ListHealthTargets(subscriptionID, after uint, limit int, out *[]SourceHealthTarget) error
}
