//go:build !minimal

package remotesubservice

import (
	"context"
	"sync"
	"time"

	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

var refreshMu sync.Mutex

func (s *Service) RefreshDueSubscriptions(loginUser string) (int, error) {
	return s.RefreshDueSubscriptionsContext(context.Background(), loginUser)
}

func (s *Service) RefreshDueSubscriptionsContext(ctx context.Context, loginUser string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	refreshMu.Lock()
	defer refreshMu.Unlock()

	subscriptions, err := remotesub.DueSubscriptions(dbsqlite.DB(), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	refreshed := 0
	for _, subscription := range subscriptions {
		if err := ctx.Err(); err != nil {
			return refreshed, err
		}
		if _, err := s.refreshSubscription(ctx, subscription.Id, loginUser); err != nil {
			logger.Warning("remote subscription auto refresh failed: ", subscription.Name, ": ", err)
			continue
		}
		refreshed++
	}
	return refreshed, nil
}
