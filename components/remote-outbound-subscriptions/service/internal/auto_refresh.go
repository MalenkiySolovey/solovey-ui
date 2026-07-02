//go:build !minimal

package remotesubservice

import (
	"sync"
	"time"

	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

var refreshMu sync.Mutex

func (s *Service) RefreshDueSubscriptions(loginUser string) (int, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	subscriptions, err := remotesub.DueSubscriptions(dbsqlite.DB(), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	refreshed := 0
	for _, subscription := range subscriptions {
		if _, err := s.refreshSubscription(subscription.Id, loginUser); err != nil {
			logger.Warning("remote subscription auto refresh failed: ", subscription.Name, ": ", err)
			continue
		}
		refreshed++
	}
	return refreshed, nil
}
