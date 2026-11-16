//go:build !minimal

package remotesubservice

import (
	"context"
	"errors"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	"strings"
	"testing"
)

type refreshStoreFixture struct {
	Store
	ctx     context.Context
	source  remotesub.RemoteOutboundSubscription
	loads   int
	changed bool
	marks   int
	mark    string
}

func (s *refreshStoreFixture) OperationContext(ctx context.Context) context.Context { return ctx }
func (s *refreshStoreFixture) WithContext(ctx context.Context) Store                { s.ctx = ctx; return s }
func (s *refreshStoreFixture) LoadSubscription(_ uint, out *remotesub.RemoteOutboundSubscription) error {
	s.loads++
	*out = s.source
	if s.changed && s.loads > 1 {
		out.Url = "https://replacement.example.org/source"
	}
	return nil
}
func (s *refreshStoreFixture) Transaction(call func(Store) error) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return call(s)
}
func (s *refreshStoreFixture) MarkRefreshError(_ uint, message string, _ int64) error {
	s.marks++
	s.mark = message
	return nil
}

type sourceFetcherFixture func(context.Context, Store, remotesub.RemoteOutboundSubscription, remotesub.FetchOptions) (*remotesub.FetchedSubscription, error)

func (f sourceFetcherFixture) FetchSource(ctx context.Context, s Store, sub remotesub.RemoteOutboundSubscription, o remotesub.FetchOptions) (*remotesub.FetchedSubscription, error) {
	return f(ctx, s, sub, o)
}
func TestRefreshFailureKeepsAcceptedSnapshotAndRedactsDiagnostic(t *testing.T) {
	store := &refreshStoreFixture{source: remotesub.RemoteOutboundSubscription{Id: 1, Url: "https://source.example.org", CanonicalSnapshot: []byte("accepted-snapshot")}}
	failed := errors.New("GET https://user:password@source.example.org/private-token?key=credential failed")
	service := Service{Store: store, Fetcher: sourceFetcherFixture(func(context.Context, Store, remotesub.RemoteOutboundSubscription, remotesub.FetchOptions) (*remotesub.FetchedSubscription, error) {
		return nil, failed
	})}
	_, err := service.RefreshSubscriptionContext(context.Background(), 1, "fixture")
	if !errors.Is(err, failed) || store.marks != 1 || string(store.source.CanonicalSnapshot) != "accepted-snapshot" {
		t.Fatal("failed fetch changed accepted state or omitted error evidence")
	}
	for _, secret := range []string{"password", "private-token", "credential"} {
		if strings.Contains(store.mark, secret) {
			t.Fatal("durable error leaked source credentials")
		}
	}
}
func TestRefreshRejectsReplacementSourceAfterSuccessfulOrFailedFetch(t *testing.T) {
	for _, failed := range []bool{false, true} {
		store := &refreshStoreFixture{changed: true, source: remotesub.RemoteOutboundSubscription{Id: 1, Url: "https://source.example.org"}}
		service := Service{Store: store, Fetcher: sourceFetcherFixture(func(context.Context, Store, remotesub.RemoteOutboundSubscription, remotesub.FetchOptions) (*remotesub.FetchedSubscription, error) {
			if failed {
				return nil, errors.New("fetch failed")
			}
			return &remotesub.FetchedSubscription{}, nil
		})}
		_, err := service.RefreshSubscriptionContext(context.Background(), 1, "fixture")
		if err == nil || !strings.Contains(err.Error(), "SOURCE_CHANGED_RETRY") || store.marks != 0 {
			t.Fatalf("stale fetch overwrote replacement state: %v", err)
		}
	}
}
func TestCanceledRefreshCannotCommitErrorOrSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &refreshStoreFixture{source: remotesub.RemoteOutboundSubscription{Id: 1, Url: "https://source.example.org"}}
	service := Service{Store: store, Fetcher: sourceFetcherFixture(func(context.Context, Store, remotesub.RemoteOutboundSubscription, remotesub.FetchOptions) (*remotesub.FetchedSubscription, error) {
		cancel()
		return nil, context.Canceled
	})}
	_, err := service.RefreshSubscriptionContext(ctx, 1, "fixture")
	if !errors.Is(err, context.Canceled) || store.marks != 0 {
		t.Fatalf("canceled refresh committed: %v", err)
	}
}
