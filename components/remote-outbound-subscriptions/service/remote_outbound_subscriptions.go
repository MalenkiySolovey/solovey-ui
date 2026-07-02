//go:build !minimal

package service

import (
	"context"

	remotedomain "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	remotesettings "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/internal/settings"
	remotesubservice "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/service/internal"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"

	"gorm.io/gorm"
)

const defaultRemoteOutboundGroupName = remotedomain.DefaultGroupName

type RemoteOutboundService struct {
	Runtime *coreservice.Runtime
}

func (s *RemoteOutboundService) runtime() *coreservice.Runtime {
	if s != nil {
		if s.Runtime != nil {
			return s.Runtime
		}
	}
	return coreservice.DefaultRuntime()
}

func (s *RemoteOutboundService) implementation() *remotesubservice.Service {
	return &remotesubservice.Service{
		Effects:  remoteOutboundEffects{runtime: s.runtime()},
		Settings: remotesettings.Reader{},
	}
}

type remoteOutboundEffects struct {
	runtime *coreservice.Runtime
}

func (e remoteOutboundEffects) RecordChange(tx *gorm.DB, loginUser string, action string, payload any) error {
	return (&coreservice.ConfigService{Runtime: e.runtime}).RecordComponentConfigChange(tx, loginUser, "remoteOutboundSubscriptions", action, payload)
}

func (e remoteOutboundEffects) Invalidate(loginUser string, coreRestart bool) {
	configService := coreservice.NewConfigServiceWithRuntime(e.runtime)
	configService.ApplyComponentConfigChangeEffects(coreservice.ComponentConfigChangeEffects{
		PrimaryObject: "remoteOutboundSubscriptions",
		IncludeObjects: []string{
			"outbounds",
			"config",
		},
		CoreRestart: coreRestart,
	})
}

func (s *RemoteOutboundService) GetAll() (*[]remotedomain.RemoteOutboundSubscription, error) {
	return s.implementation().GetAll()
}

func (s *RemoteOutboundService) SaveSubscription(input remotedomain.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*remotedomain.RemoteOutboundSubscription, error) {
	return s.implementation().SaveSubscription(input, enabledProvided, loginUser)
}

func (s *RemoteOutboundService) GetSubscription(id uint) (*remotedomain.RemoteOutboundSubscription, error) {
	return s.implementation().GetSubscription(id)
}

func (s *RemoteOutboundService) GetCollectedData(id uint) (*remotesubservice.CollectedSubscriptionData, error) {
	return s.implementation().GetCollectedData(id)
}

func (s *RemoteOutboundService) DeleteSubscription(id uint, loginUser string) error {
	return s.implementation().DeleteSubscription(id, loginUser)
}

func (s *RemoteOutboundService) RefreshSubscription(id uint, loginUser string) (*remotedomain.RefreshResult, error) {
	return s.implementation().RefreshSubscription(id, loginUser)
}

func (s *RemoteOutboundService) SyncConnectionToOutbound(id uint, loginUser string) (*remotedomain.RemoteOutboundConnection, error) {
	return s.implementation().SyncConnectionToOutbound(id, loginUser)
}

func (s *RemoteOutboundService) SaveGroup(input remotedomain.RemoteOutboundGroup, enabledProvided bool, loginUser string) (*remotedomain.RemoteOutboundGroup, error) {
	return s.implementation().SaveGroup(input, enabledProvided, loginUser)
}

func (s *RemoteOutboundService) SaveGroupForAllSubscriptions(name string, loginUser string) (*remotesubservice.BulkGroupResult, error) {
	return s.implementation().SaveGroupForAllSubscriptions(name, loginUser)
}

func (s *RemoteOutboundService) DeleteGroup(id uint, loginUser string) error {
	return s.implementation().DeleteGroup(id, loginUser)
}

func (s *RemoteOutboundService) MoveConnectionToGroup(connectionID uint, groupID uint, loginUser string) error {
	return s.implementation().MoveConnectionToGroup(connectionID, groupID, loginUser)
}

func (s *RemoteOutboundService) SetGroupConnections(groupID uint, connectionIDs []uint, loginUser string) error {
	return s.implementation().SetGroupConnections(groupID, connectionIDs, loginUser)
}

func (s *RemoteOutboundService) ToggleGroupOutbounds(groupID uint, loginUser string) (*remotedomain.GroupActionResult, error) {
	return s.implementation().ToggleGroupOutbounds(groupID, loginUser)
}

func (s *RemoteOutboundService) CheckConnection(ctx context.Context, id uint, target string) (*remotedomain.CheckResult, error) {
	return s.implementation().CheckConnection(ctx, id, target)
}

func (s *RemoteOutboundService) CheckSubscription(ctx context.Context, subscriptionID uint, target string) ([]remotedomain.CheckResult, error) {
	return s.implementation().CheckSubscription(ctx, subscriptionID, target)
}

func (s *RemoteOutboundService) CheckAll(ctx context.Context, target string) ([]remotedomain.CheckResult, error) {
	return s.implementation().CheckAll(ctx, target)
}

func (s *RemoteOutboundService) RefreshDueSubscriptions(loginUser string) (int, error) {
	return s.implementation().RefreshDueSubscriptions(loginUser)
}
