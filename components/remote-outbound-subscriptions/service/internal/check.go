//go:build !minimal

package remotesubservice

import (
	"context"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func (s *Service) CheckConnection(ctx context.Context, id uint, target string) (*remotesub.CheckResult, error) {
	return remotesub.CheckConnection(ctx, dbsqlite.DB(), id, target)
}
func (s *Service) CheckSubscription(ctx context.Context, subscriptionID uint, target string) ([]remotesub.CheckResult, error) {
	return remotesub.CheckSubscription(ctx, dbsqlite.DB(), subscriptionID, target)
}
func (s *Service) CheckAll(ctx context.Context, target string) ([]remotesub.CheckResult, error) {
	return remotesub.CheckAll(ctx, dbsqlite.DB(), target)
}
