//go:build !minimal

package remotesubservice

import (
	"context"
	feature "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions/service"
	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
)

func (s *Service) RefreshDueSubscriptions(loginUser string) (int, error) {
	return s.implementation().RefreshDueSubscriptions(loginUser)
}
func (s *Service) RefreshDueSubscriptionsContext(ctx context.Context, loginUser string) (int, error) {
	return s.implementation().RefreshDueSubscriptionsContext(ctx, loginUser)
}
func (s *Service) CheckConnection(ctx context.Context, id uint, target string) (*remotesub.CheckResult, error) {
	return s.implementation().CheckConnection(ctx, id, target)
}
func (s *Service) CheckSubscription(ctx context.Context, subscriptionID uint, target string) ([]remotesub.CheckResult, error) {
	return s.implementation().CheckSubscription(ctx, subscriptionID, target)
}
func (s *Service) CheckAll(ctx context.Context, target string) ([]remotesub.CheckResult, error) {
	return s.implementation().CheckAll(ctx, target)
}
func (s *Service) GetCollectedData(id uint) (*CollectedSubscriptionData, error) {
	return s.implementation().GetCollectedData(id)
}
func (s *Service) SaveGroup(input remotesub.RemoteOutboundGroup, enabledProvided bool, loginUser string) (*remotesub.RemoteOutboundGroup, error) {
	return s.implementation().SaveGroup(input, enabledProvided, loginUser)
}
func (s *Service) SaveGroupForAllSubscriptions(name string, loginUser string) (*BulkGroupResult, error) {
	return s.implementation().SaveGroupForAllSubscriptions(name, loginUser)
}
func (s *Service) DeleteGroup(id uint, loginUser string) error {
	return s.implementation().DeleteGroup(id, loginUser)
}
func (s *Service) MoveConnectionToGroup(connectionID uint, groupID uint, loginUser string) error {
	return s.implementation().MoveConnectionToGroup(connectionID, groupID, loginUser)
}
func (s *Service) SetGroupConnections(groupID uint, connectionIDs []uint, loginUser string) error {
	return s.implementation().SetGroupConnections(groupID, connectionIDs, loginUser)
}
func (s *Service) ToggleGroupOutbounds(groupID uint, loginUser string) (*remotesub.GroupActionResult, error) {
	return s.implementation().ToggleGroupOutbounds(groupID, loginUser)
}
func (s *Service) ProbeDueSubscriptionsContext(ctx context.Context, schedule *HealthSchedule) (int, error) {
	return s.implementation().ProbeDueSubscriptionsContext(ctx, schedule)
}
func (s *Service) GetAll() (*[]remotesub.RemoteOutboundSubscription, error) {
	return s.implementation().GetAll()
}
func (s *Service) SaveSubscription(input remotesub.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*remotesub.RemoteOutboundSubscription, error) {
	return s.implementation().SaveSubscription(input, enabledProvided, loginUser)
}
func (s *Service) GetSubscription(id uint) (*remotesub.RemoteOutboundSubscription, error) {
	return s.implementation().GetSubscription(id)
}
func (s *Service) DeleteSubscription(id uint, loginUser string) error {
	return s.implementation().DeleteSubscription(id, loginUser)
}
func (s *Service) RefreshSubscriptionContext(ctx context.Context, id uint, loginUser string) (*remotesub.RefreshResult, error) {
	return s.implementation().RefreshSubscriptionContext(ctx, id, loginUser)
}
func (s *Service) SyncConnectionToOutbound(id uint, loginUser string) (*remotesub.RemoteOutboundConnection, error) {
	return s.implementation().SyncConnectionToOutbound(id, loginUser)
}

type CollectedSubscriptionData = feature.CollectedSubscriptionData
type CollectedProfileBlock = feature.CollectedProfileBlock
type CollectedProfileCharacteristic = feature.CollectedProfileCharacteristic
type CollectedProfileValue = feature.CollectedProfileValue
type CollectedConnectionData = feature.CollectedConnectionData
type CanonicalSnapshotView = feature.CanonicalSnapshotView
type CanonicalConnectionView = feature.CanonicalConnectionView
type BulkGroupResult = feature.BulkGroupResult
type HealthSchedule = feature.HealthSchedule
type SourceHealthTarget = feature.SourceHealthTarget
