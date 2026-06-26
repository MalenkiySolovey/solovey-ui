package remotesubservice

import (
	"context"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
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
