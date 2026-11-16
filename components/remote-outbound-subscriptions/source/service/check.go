//go:build !minimal

package remotesubservice

import (
	"context"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
)

func (s *Service) CheckConnection(ctx context.Context, id uint, target string) (*remotesub.CheckResult, error) {
	return s.Checks.CheckConnection(ctx, id, target)
}
func (s *Service) CheckSubscription(ctx context.Context, subscriptionID uint, target string) ([]remotesub.CheckResult, error) {
	return s.Checks.CheckSubscription(ctx, subscriptionID, target)
}
func (s *Service) CheckAll(ctx context.Context, target string) ([]remotesub.CheckResult, error) {
	return s.Checks.CheckAll(ctx, target)
}
