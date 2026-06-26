package remotesubservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type Effects interface {
	RecordChange(tx *gorm.DB, loginUser string, action string, payload any) error
	Invalidate(loginUser string, coreRestart bool)
}

type Settings interface {
	GetSubRemoteGroupAdaptation() (string, error)
	GetSubRemoteConversionPolicy() (string, error)
}

type Service struct {
	Effects  Effects
	Settings Settings
}

func (s *Service) GetAll() (*[]model.RemoteOutboundSubscription, error) {
	db := dbsqlite.DB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.BackfillGroupMemberships(tx); err != nil {
			return err
		}
		return remotesub.ReconcileGroupStates(tx)
	}); err != nil {
		return nil, err
	}
	var subscriptions []model.RemoteOutboundSubscription
	err := db.
		Preload("Groups", func(db *gorm.DB) *gorm.DB {
			return db.Order(entityorder.Clause)
		}).
		Preload("Connections", func(db *gorm.DB) *gorm.DB {
			return db.Order(entityorder.Clause)
		}).
		Order(entityorder.Clause).
		Find(&subscriptions).Error
	if err != nil {
		return nil, err
	}
	if err := remotesub.HydrateConnectionGroupIDs(db, subscriptions); err != nil {
		return nil, err
	}
	remotesub.FilterVisibleConnections(subscriptions)
	remotesub.HydrateConnectionTypeInfo(subscriptions)
	return &subscriptions, nil
}
func (s *Service) SaveSubscription(input model.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*model.RemoteOutboundSubscription, error) {
	now := time.Now().Unix()
	input.Name = strings.TrimSpace(input.Name)
	input.Url = strings.TrimSpace(input.Url)
	input.TagPrefix = remotesub.SanitizeTagPrefix(input.TagPrefix)
	input.UpdateInterval = remotesub.NormalizeUpdateInterval(input.UpdateInterval)
	if input.Name == "" {
		return nil, common.NewError("subscription name can not be empty")
	}
	if input.Url == "" {
		return nil, common.NewError("subscription url can not be empty")
	}
	if err := remotesub.ValidateSubscriptionURL(input.Url); err != nil {
		return nil, common.NewError("subscription url is not allowed: ", err)
	}
	if input.TagPrefix == "" {
		input.TagPrefix = remotesub.DefaultTagPrefix(input.Name, input.Id)
	}
	if !enabledProvided && input.Id == 0 {
		input.Enabled = true
	}
	db := dbsqlite.DB()
	var saved model.RemoteOutboundSubscription
	coreRestart := false
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.ReconcileGroupStates(tx); err != nil {
			return err
		}
		sortOrder, err := entityorder.ForSave(tx, &model.RemoteOutboundSubscription{}, input.Id)
		if err != nil {
			return err
		}
		if input.Id != 0 {
			if err := tx.First(&saved, input.Id).Error; err != nil {
				return err
			}
			oldTagPrefix := saved.TagPrefix
			saved.Name = input.Name
			saved.Url = input.Url
			saved.Enabled = input.Enabled
			saved.TagPrefix = input.TagPrefix
			saved.AutoUpdate = input.AutoUpdate
			saved.UpdateInterval = input.UpdateInterval
			saved.SortOrder = sortOrder
			saved.UpdatedAt = now
			if oldTagPrefix != saved.TagPrefix {
				changed, err := remotesub.RetagSubscriptionConnections(tx, &saved)
				if err != nil {
					return err
				}
				coreRestart = changed
			}
		} else {
			saved = model.RemoteOutboundSubscription{
				SortOrder:      sortOrder,
				Name:           input.Name,
				Url:            input.Url,
				Enabled:        input.Enabled,
				TagPrefix:      input.TagPrefix,
				AutoUpdate:     input.AutoUpdate,
				UpdateInterval: input.UpdateInterval,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if strings.Contains(saved.TagPrefix, "{id}") {
			saved.TagPrefix = strings.ReplaceAll(saved.TagPrefix, "{id}", fmt.Sprintf("%d", saved.Id))
			if err := tx.Save(&saved).Error; err != nil {
				return err
			}
		}
		if _, err := remotesub.EnsureDefaultGroup(tx, saved.Id, now); err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "subscription_save", saved)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	loaded, err := s.GetSubscription(saved.Id)
	if err != nil {
		return nil, err
	}
	return loaded, nil
}
func (s *Service) GetSubscription(id uint) (*model.RemoteOutboundSubscription, error) {
	var subscription model.RemoteOutboundSubscription
	err := dbsqlite.DB().
		Preload("Groups", func(db *gorm.DB) *gorm.DB {
			return db.Order(entityorder.Clause)
		}).
		Preload("Connections", func(db *gorm.DB) *gorm.DB {
			return db.Order(entityorder.Clause)
		}).
		First(&subscription, id).Error
	if err != nil {
		return nil, err
	}
	subscriptions := []model.RemoteOutboundSubscription{subscription}
	if err := remotesub.HydrateConnectionGroupIDs(dbsqlite.DB(), subscriptions); err != nil {
		return nil, err
	}
	remotesub.FilterVisibleConnections(subscriptions)
	remotesub.HydrateConnectionTypeInfo(subscriptions)
	subscription = subscriptions[0]
	return &subscription, nil
}
func (s *Service) DeleteSubscription(id uint, loginUser string) error {
	if id == 0 {
		return common.NewError("subscription id is required")
	}
	coreRestart := false
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var subscription model.RemoteOutboundSubscription
		if err := tx.First(&subscription, id).Error; err != nil {
			return err
		}
		outboundChanged, err := remotesub.DeleteSyncedOutboundsForSubscription(tx, subscription.Id)
		if err != nil {
			return err
		}
		coreRestart = coreRestart || outboundChanged
		if err := s.recordRemoteOutboundChange(tx, loginUser, "subscription_delete", subscription); err != nil {
			return err
		}
		if err := tx.
			Where("connection_id IN (SELECT id FROM remote_outbound_connections WHERE subscription_id = ?)", subscription.Id).
			Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
			return err
		}
		if err := tx.Where("subscription_id = ?", subscription.Id).Delete(&model.RemoteOutboundConnection{}).Error; err != nil {
			return err
		}
		if err := tx.Where("subscription_id = ?", subscription.Id).Delete(&model.RemoteOutboundGroup{}).Error; err != nil {
			return err
		}
		return tx.Delete(&subscription).Error
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return nil
}
func (s *Service) RefreshSubscription(id uint, loginUser string) (*remotesub.RefreshResult, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	return s.refreshSubscription(id, loginUser)
}
func (s *Service) refreshSubscription(id uint, loginUser string) (*remotesub.RefreshResult, error) {
	if id == 0 {
		return nil, common.NewError("subscription id is required")
	}
	var subscription model.RemoteOutboundSubscription
	db := dbsqlite.DB()
	if err := db.First(&subscription, id).Error; err != nil {
		return nil, err
	}
	fetched, fetchErr := remotesub.FetchSubscriptionWithOptions(subscription.Url, s.fetchOptions())
	now := time.Now().Unix()
	if fetchErr != nil {
		// Best-effort: persist the last error for the UI even if this update
		// fails; the fetch error itself is what gets returned below.
		_ = remotesub.MarkRefreshError(db, id, fetchErr.Error(), now)
		return nil, fetchErr
	}
	var result remotesub.RefreshResult
	syncedChanged := false
	err := db.Transaction(func(tx *gorm.DB) error {
		refreshResult, coreChanged, err := remotesub.RefreshFetchedSubscription(tx, &subscription, fetched, now)
		if err != nil {
			return err
		}
		result = refreshResult
		syncedChanged = coreChanged
		return s.recordRemoteOutboundChange(tx, loginUser, "subscription_refresh", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, syncedChanged)
	return &result, nil
}

func (s *Service) fetchOptions() remotesub.FetchOptions {
	if s.Settings == nil {
		return remotesub.FetchOptions{ConversionPolicy: subconversion.DefaultPolicy()}
	}
	groupAdaptation, err := s.Settings.GetSubRemoteGroupAdaptation()
	if err != nil {
		groupAdaptation = ""
	}
	rawPolicy, err := s.Settings.GetSubRemoteConversionPolicy()
	if err != nil {
		rawPolicy = ""
	}
	return remotesub.FetchOptions{
		GroupAdaptation:  groupAdaptation,
		ConversionPolicy: subconversion.ParsePolicy(rawPolicy, groupAdaptation),
	}
}

func (s *Service) SyncConnectionToOutbound(id uint, loginUser string) (*model.RemoteOutboundConnection, error) {
	if id == 0 {
		return nil, common.NewError("connection id is required")
	}
	var saved model.RemoteOutboundConnection
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
			return err
		}
		if err := remotesub.ReconcileGroupStates(tx); err != nil {
			return err
		}
		if err := tx.First(&saved, id).Error; err != nil {
			return err
		}
		if err := remotesub.SyncConnectionToOutbound(tx, &saved, true); err != nil {
			return err
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "connection_sync", saved)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, true)
	return &saved, nil
}
func (s *Service) recordRemoteOutboundChange(tx *gorm.DB, loginUser string, act string, payload any) error {
	if s.Effects == nil {
		return common.NewError("remote outbound effects are not configured")
	}
	return s.Effects.RecordChange(tx, loginUser, act, payload)
}
func (s *Service) invalidateRemoteOutboundData(loginUser string, coreRestart bool) {
	if s.Effects != nil {
		s.Effects.Invalidate(loginUser, coreRestart)
	}
}
