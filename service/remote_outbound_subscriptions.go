package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	remotedomain "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	remotesubservice "github.com/MalenkiySolovey/solovey-ui/service/remotesub"

	"gorm.io/gorm"
)

const defaultRemoteOutboundGroupName = remotedomain.DefaultGroupName

type RemoteOutboundService struct {
	Runtime *Runtime
}

func (s *RemoteOutboundService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *RemoteOutboundService) implementation() *remotesubservice.Service {
	return &remotesubservice.Service{
		Effects:  remoteOutboundEffects{runtime: s.runtime()},
		Settings: &SettingService{},
	}
}

type remoteOutboundEffects struct {
	runtime *Runtime
}

func (e remoteOutboundEffects) RecordChange(tx *gorm.DB, loginUser string, action string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return (&ConfigService{Runtime: e.runtime}).recordConfigChange(tx, loginUser, "remoteOutboundSubscriptions", action, data)
}

func (e remoteOutboundEffects) Invalidate(loginUser string, coreRestart bool) {
	configService := NewConfigServiceWithRuntime(e.runtime)
	configService.setLastUpdate(time.Now().Unix())
	if !coreRestart {
		realtime.Publish(realtime.TopicConfigInvalidated, nil)
		return
	}
	plan := newConfigSavePlan("remoteOutboundSubscriptions")
	plan.IncludeObjects("outbounds", "config")
	plan.RequireCoreRestart()
	configService.applyConfigSaveEffects(plan, loginUser, false, false)
}

func (s *RemoteOutboundService) GetAll() (*[]model.RemoteOutboundSubscription, error) {
	return s.implementation().GetAll()
}

func (s *RemoteOutboundService) SaveSubscription(input model.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*model.RemoteOutboundSubscription, error) {
	return s.implementation().SaveSubscription(input, enabledProvided, loginUser)
}

func (s *RemoteOutboundService) GetSubscription(id uint) (*model.RemoteOutboundSubscription, error) {
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

func (s *RemoteOutboundService) SyncConnectionToOutbound(id uint, loginUser string) (*model.RemoteOutboundConnection, error) {
	return s.implementation().SyncConnectionToOutbound(id, loginUser)
}

func (s *RemoteOutboundService) SaveGroup(input model.RemoteOutboundGroup, enabledProvided bool, loginUser string) (*model.RemoteOutboundGroup, error) {
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
