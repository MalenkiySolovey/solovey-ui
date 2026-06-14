package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/MalenkiySolovey/solovey-ui/core"
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/util"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const defaultRemoteOutboundGroupName = "Default"

type RemoteOutboundService struct {
	Runtime *Runtime
}

type RemoteOutboundRefreshResult struct {
	SubscriptionId uint `json:"subscriptionId"`
	Fetched        int  `json:"fetched"`
	Created        int  `json:"created"`
	Updated        int  `json:"updated"`
	MarkedMissing  int  `json:"markedMissing"`
	Synced         int  `json:"synced"`
}

type RemoteOutboundCheckResult struct {
	ConnectionId uint                     `json:"connectionId"`
	OutboundTag  string                   `json:"outboundTag"`
	Skipped      bool                     `json:"skipped,omitempty"`
	Error        string                   `json:"error,omitempty"`
	Result       core.CheckOutboundResult `json:"result"`
}

type RemoteOutboundGroupActionResult struct {
	GroupId    uint `json:"groupId"`
	Added      int  `json:"added"`
	Removed    int  `json:"removed"`
	Skipped    int  `json:"skipped"`
	OutboundOn bool `json:"outboundOn"`
}

type remoteOutboundClientLink struct {
	Type          string `json:"type"`
	GroupId       uint   `json:"groupId"`
	RemoteGroupId uint   `json:"remoteGroupId"`
}

func (s *RemoteOutboundService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *RemoteOutboundService) configService() *ConfigService {
	return NewConfigServiceWithRuntime(s.runtime())
}

func (s *RemoteOutboundService) GetAll() (*[]model.RemoteOutboundSubscription, error) {
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.reconcileOutboundLinksTx(tx); err != nil {
			return err
		}
		if err := backfillRemoteOutboundGroupMembershipsTx(tx); err != nil {
			return err
		}
		return reconcileRemoteOutboundGroupStatesTx(tx)
	}); err != nil {
		return nil, err
	}
	var subscriptions []model.RemoteOutboundSubscription
	err := db.
		Preload("Groups", func(db *gorm.DB) *gorm.DB {
			return db.Order(sortOrderClause)
		}).
		Preload("Connections", func(db *gorm.DB) *gorm.DB {
			return db.Order(sortOrderClause)
		}).
		Order(sortOrderClause).
		Find(&subscriptions).Error
	if err != nil {
		return nil, err
	}
	if err := hydrateRemoteOutboundConnectionGroupIDs(db, subscriptions); err != nil {
		return nil, err
	}
	return &subscriptions, nil
}

func (s *RemoteOutboundService) SaveSubscription(input model.RemoteOutboundSubscription, enabledProvided bool, loginUser string) (*model.RemoteOutboundSubscription, error) {
	now := time.Now().Unix()
	input.Name = strings.TrimSpace(input.Name)
	input.Url = strings.TrimSpace(input.Url)
	input.TagPrefix = sanitizeRemoteOutboundTagPrefix(input.TagPrefix)
	input.UpdateInterval = normalizeRemoteOutboundUpdateInterval(input.UpdateInterval)
	if input.Name == "" {
		return nil, common.NewError("subscription name can not be empty")
	}
	if input.Url == "" {
		return nil, common.NewError("subscription url can not be empty")
	}
	if err := util.ValidateExternalURL(input.Url); err != nil {
		return nil, common.NewError("subscription url is not allowed: ", err)
	}
	if input.TagPrefix == "" {
		input.TagPrefix = defaultRemoteOutboundTagPrefix(input.Name, input.Id)
	}
	if !enabledProvided && input.Id == 0 {
		input.Enabled = true
	}

	db := database.GetDB()
	var saved model.RemoteOutboundSubscription
	coreRestart := false
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.reconcileOutboundLinksTx(tx); err != nil {
			return err
		}
		if err := reconcileRemoteOutboundGroupStatesTx(tx); err != nil {
			return err
		}
		sortOrder, err := sortOrderForSave(tx, &model.RemoteOutboundSubscription{}, input.Id)
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
				changed, err := s.retagSubscriptionConnectionsTx(tx, &saved)
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
		if _, err := ensureRemoteOutboundDefaultGroup(tx, saved.Id, now); err != nil {
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

func (s *RemoteOutboundService) GetSubscription(id uint) (*model.RemoteOutboundSubscription, error) {
	var subscription model.RemoteOutboundSubscription
	err := database.GetDB().
		Preload("Groups", func(db *gorm.DB) *gorm.DB {
			return db.Order(sortOrderClause)
		}).
		Preload("Connections", func(db *gorm.DB) *gorm.DB {
			return db.Order(sortOrderClause)
		}).
		First(&subscription, id).Error
	if err != nil {
		return nil, err
	}
	subscriptions := []model.RemoteOutboundSubscription{subscription}
	if err := hydrateRemoteOutboundConnectionGroupIDs(database.GetDB(), subscriptions); err != nil {
		return nil, err
	}
	subscription = subscriptions[0]
	return &subscription, nil
}

func (s *RemoteOutboundService) DeleteSubscription(id uint, loginUser string) error {
	if id == 0 {
		return common.NewError("subscription id is required")
	}
	coreRestart := false
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var subscription model.RemoteOutboundSubscription
		if err := tx.First(&subscription, id).Error; err != nil {
			return err
		}
		var connections []model.RemoteOutboundConnection
		if err := tx.
			Where("subscription_id = ? AND synced = ?", subscription.Id, true).
			Find(&connections).Error; err != nil {
			return err
		}
		for _, connection := range connections {
			if connection.OutboundId == nil || *connection.OutboundId == 0 {
				continue
			}
			result := tx.Where("id = ? AND tag = ?", *connection.OutboundId, connection.OutboundTag).Delete(&model.Outbound{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				coreRestart = true
			}
		}
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

func (s *RemoteOutboundService) SaveGroup(input model.RemoteOutboundGroup, enabledProvided bool, loginUser string) (*model.RemoteOutboundGroup, error) {
	now := time.Now().Unix()
	input.Name = strings.TrimSpace(input.Name)
	if input.SubscriptionId == 0 {
		return nil, common.NewError("subscription id is required")
	}
	if input.Name == "" {
		return nil, common.NewError("group name can not be empty")
	}
	if !enabledProvided && input.Id == 0 {
		input.Enabled = true
	}

	var saved model.RemoteOutboundGroup
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", input.SubscriptionId).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return common.NewError("subscription not found")
		}
		sortOrder, err := sortOrderForSave(tx, &model.RemoteOutboundGroup{}, input.Id)
		if err != nil {
			return err
		}
		if input.Id != 0 {
			if err := tx.First(&saved, input.Id).Error; err != nil {
				return err
			}
			if saved.SubscriptionId != input.SubscriptionId {
				return common.NewError("group does not belong to subscription")
			}
			saved.Name = input.Name
			saved.Enabled = input.Enabled
			saved.SortOrder = sortOrder
			saved.UpdatedAt = now
		} else {
			saved = model.RemoteOutboundGroup{
				SubscriptionId: input.SubscriptionId,
				SortOrder:      sortOrder,
				Name:           input.Name,
				Enabled:        input.Enabled,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
		}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "group_save", saved)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return &saved, nil
}

func (s *RemoteOutboundService) DeleteGroup(id uint, loginUser string) error {
	if id == 0 {
		return common.NewError("group id is required")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var group model.RemoteOutboundGroup
		if err := tx.First(&group, id).Error; err != nil {
			return err
		}
		defaultGroup, err := ensureRemoteOutboundDefaultGroup(tx, group.SubscriptionId, time.Now().Unix())
		if err != nil {
			return err
		}
		if group.Id == defaultGroup.Id {
			return common.NewError("default group can not be deleted")
		}
		var affected []model.RemoteOutboundGroupConnection
		if err := tx.Where("group_id = ?", group.Id).Find(&affected).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", group.Id).Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
			return err
		}
		for _, link := range affected {
			if err := ensureRemoteOutboundConnectionHasGroupTx(tx, link.ConnectionId, defaultGroup.Id); err != nil {
				return err
			}
		}
		if err := s.recordRemoteOutboundChange(tx, loginUser, "group_delete", group); err != nil {
			return err
		}
		return tx.Delete(&group).Error
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return nil
}

func (s *RemoteOutboundService) MoveConnectionToGroup(connectionID uint, groupID uint, loginUser string) error {
	if connectionID == 0 || groupID == 0 {
		return common.NewError("connection id and group id are required")
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var connection model.RemoteOutboundConnection
		if err := tx.First(&connection, connectionID).Error; err != nil {
			return err
		}
		var group model.RemoteOutboundGroup
		if err := tx.First(&group, groupID).Error; err != nil {
			return err
		}
		if group.SubscriptionId != connection.SubscriptionId {
			return common.NewError("group does not belong to connection subscription")
		}
		connection.GroupId = group.Id
		connection.UpdatedAt = time.Now().Unix()
		if err := tx.Save(&connection).Error; err != nil {
			return err
		}
		if err := tx.Where("connection_id = ?", connection.Id).Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
			return err
		}
		if err := addRemoteOutboundGroupConnectionTx(tx, group.Id, connection.Id, time.Now().Unix()); err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "connection_group", connection)
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return nil
}

func (s *RemoteOutboundService) SetGroupConnections(groupID uint, connectionIDs []uint, loginUser string) error {
	if groupID == 0 {
		return common.NewError("group id is required")
	}
	coreRestart := false
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var group model.RemoteOutboundGroup
		if err := tx.First(&group, groupID).Error; err != nil {
			return err
		}
		defaultGroup, err := ensureRemoteOutboundDefaultGroup(tx, group.SubscriptionId, time.Now().Unix())
		if err != nil {
			return err
		}
		isDefaultGroup := group.Id == defaultGroup.Id
		selected := make(map[uint]struct{}, len(connectionIDs))
		for _, id := range connectionIDs {
			if id == 0 {
				return common.NewError("connection id can not be empty")
			}
			selected[id] = struct{}{}
		}
		if len(selected) > 0 {
			var count int64
			if err := tx.Model(&model.RemoteOutboundConnection{}).
				Where("subscription_id = ? AND id IN ?", group.SubscriptionId, mapKeys(selected)).
				Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(selected)) {
				return common.NewError("some connections do not belong to this subscription")
			}
		}

		current, err := remoteOutboundGroupConnectionIDSetTx(tx, group.Id)
		if err != nil {
			return err
		}
		removed := make([]uint, 0)
		for id := range current {
			if _, ok := selected[id]; !ok {
				removed = append(removed, id)
			}
		}
		added := make([]uint, 0)
		for id := range selected {
			if _, ok := current[id]; !ok {
				added = append(added, id)
			}
		}

		if len(removed) > 0 {
			if err := tx.
				Where("group_id = ? AND connection_id IN ?", group.Id, removed).
				Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
				return err
			}
			for _, connectionID := range removed {
				if !isDefaultGroup {
					if err := ensureRemoteOutboundConnectionHasGroupTx(tx, connectionID, defaultGroup.Id); err != nil {
						return err
					}
				}
				if err := syncRemoteOutboundConnectionLegacyGroupIDTx(tx, connectionID); err != nil {
					return err
				}
				if group.OutboundEnabled {
					changed, err := s.unsyncRemoteConnectionIfNoOutboundGroupTx(tx, connectionID)
					if err != nil {
						return err
					}
					coreRestart = coreRestart || changed
				}
			}
		}

		now := time.Now().Unix()
		for _, connectionID := range added {
			if err := addRemoteOutboundGroupConnectionTx(tx, group.Id, connectionID, now); err != nil {
				return err
			}
			if group.OutboundEnabled {
				var connection model.RemoteOutboundConnection
				if err := tx.First(&connection, connectionID).Error; err != nil {
					return err
				}
				if connection.Enabled && !connection.Missing {
					wasSynced := connection.Synced
					if err := s.syncConnectionToOutboundTx(tx, &connection, true); err != nil {
						return err
					}
					if err := tx.Save(&connection).Error; err != nil {
						return err
					}
					coreRestart = coreRestart || !wasSynced
				}
			}
		}
		if err := syncRemoteOutboundLegacyGroupIDsTx(tx, group.SubscriptionId); err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "group_connections", map[string]any{
			"groupId":       group.Id,
			"connectionIds": connectionIDs,
		})
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return nil
}

func (s *RemoteOutboundService) ToggleGroupOutbounds(groupID uint, loginUser string) (*RemoteOutboundGroupActionResult, error) {
	if groupID == 0 {
		return nil, common.NewError("group id is required")
	}
	result := &RemoteOutboundGroupActionResult{GroupId: groupID}
	coreRestart := false
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if _, err := s.reconcileOutboundLinksTx(tx); err != nil {
			return err
		}
		if err := reconcileRemoteOutboundGroupStatesTx(tx); err != nil {
			return err
		}
		var group model.RemoteOutboundGroup
		if err := tx.First(&group, groupID).Error; err != nil {
			return err
		}
		connections, err := remoteGroupConnections(tx, group.Id)
		if err != nil {
			return err
		}
		active := filterUsableRemoteConnections(connections)
		enableOutbounds := !group.OutboundEnabled
		if enableOutbounds && len(active) == 0 {
			return common.NewError("group has no usable connections")
		}
		if !enableOutbounds {
			group.OutboundEnabled = false
			group.UpdatedAt = time.Now().Unix()
			if err := tx.Save(&group).Error; err != nil {
				return err
			}
			for index := range connections {
				if !connections[index].Synced {
					result.Skipped++
					continue
				}
				changed, err := s.unsyncRemoteConnectionIfNoOutboundGroupTx(tx, connections[index].Id)
				if err != nil {
					return err
				}
				if changed {
					result.Removed++
				} else {
					result.Skipped++
				}
			}
			result.OutboundOn = false
		} else {
			group.OutboundEnabled = true
			group.UpdatedAt = time.Now().Unix()
			if err := tx.Save(&group).Error; err != nil {
				return err
			}
			for index := range active {
				if active[index].Synced {
					result.Skipped++
					continue
				}
				if err := s.syncConnectionToOutboundTx(tx, &active[index], true); err != nil {
					return err
				}
				if err := tx.Save(&active[index]).Error; err != nil {
					return err
				}
				result.Added++
			}
			result.OutboundOn = true
		}
		coreRestart = result.Added > 0 || result.Removed > 0
		return s.recordRemoteOutboundChange(tx, loginUser, "group_outbounds", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return result, nil
}

func (s *RemoteOutboundService) RefreshSubscription(id uint, loginUser string) (*RemoteOutboundRefreshResult, error) {
	remoteOutboundRefreshMu.Lock()
	defer remoteOutboundRefreshMu.Unlock()
	return s.refreshSubscription(id, loginUser)
}

func (s *RemoteOutboundService) refreshSubscription(id uint, loginUser string) (*RemoteOutboundRefreshResult, error) {
	if id == 0 {
		return nil, common.NewError("subscription id is required")
	}

	var subscription model.RemoteOutboundSubscription
	db := database.GetDB()
	if err := db.First(&subscription, id).Error; err != nil {
		return nil, err
	}
	outbounds, fetchErr := util.GetExternalSub(subscription.Url)
	now := time.Now().Unix()
	if fetchErr != nil {
		// Best-effort: persist the last error for the UI even if this update
		// fails; the fetch error itself is what gets returned below.
		_ = db.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", id).Updates(map[string]any{
			"last_error": fetchErr.Error(),
			"updated_at": now,
		}).Error
		return nil, fetchErr
	}

	result := RemoteOutboundRefreshResult{
		SubscriptionId: subscription.Id,
		Fetched:        len(outbounds),
	}
	syncedChanged := false
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.reconcileOutboundLinksTx(tx); err != nil {
			return err
		}
		if err := reconcileRemoteOutboundGroupStatesTx(tx); err != nil {
			return err
		}
		if subscription.TagPrefix == "" {
			subscription.TagPrefix = defaultRemoteOutboundTagPrefix(subscription.Name, subscription.Id)
		}
		defaultGroup, err := ensureRemoteOutboundDefaultGroup(tx, subscription.Id, now)
		if err != nil {
			return err
		}
		if err := backfillRemoteOutboundSubscriptionGroupMembershipsTx(tx, subscription.Id); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(outbounds))
		for index, outbound := range outbounds {
			normalized, err := normalizeRemoteOutbound(outbound, index)
			if err != nil {
				return err
			}
			normalized.SourceKey = uniqueRemoteOutboundSourceKey(seen, normalized.SourceKey)
			seen[normalized.SourceKey] = struct{}{}
			var connection model.RemoteOutboundConnection
			err = tx.Where("subscription_id = ? AND source_key = ?", subscription.Id, normalized.SourceKey).First(&connection).Error
			switch {
			case err == nil:
				if updateRemoteOutboundConnection(&connection, normalized, now) {
					result.Updated++
				}
			case errors.Is(err, gorm.ErrRecordNotFound):
				tag, err := s.uniqueRemoteOutboundTag(tx, subscription, normalized.Name, 0)
				if err != nil {
					return err
				}
				sortOrder, err := nextSortOrder(tx, &model.RemoteOutboundConnection{})
				if err != nil {
					return err
				}
				connection = model.RemoteOutboundConnection{
					SubscriptionId: subscription.Id,
					GroupId:        defaultGroup.Id,
					SortOrder:      sortOrder,
					Name:           normalized.Name,
					SourceKey:      normalized.SourceKey,
					Type:           normalized.Type,
					OutboundTag:    tag,
					Enabled:        true,
					Missing:        false,
					Synced:         false,
					Options:        normalized.Options,
					LastSeen:       now,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				result.Created++
			default:
				return err
			}
			if err := tx.Save(&connection).Error; err != nil {
				return err
			}
			if connection.GroupId != 0 {
				if err := ensureRemoteOutboundConnectionHasGroupTx(tx, connection.Id, defaultGroup.Id); err != nil {
					return err
				}
			}
			if err := syncRemoteOutboundConnectionLegacyGroupIDTx(tx, connection.Id); err != nil {
				return err
			}
			shouldSync, err := remoteConnectionUsesOutboundEnabledGroupTx(tx, connection.Id)
			if err != nil {
				return err
			}
			if (connection.Synced || shouldSync) && connection.Enabled && !connection.Missing {
				wasSynced := connection.Synced
				if err := s.syncConnectionToOutboundTx(tx, &connection, false); err != nil {
					return err
				}
				if err := tx.Save(&connection).Error; err != nil {
					return err
				}
				result.Synced++
				syncedChanged = syncedChanged || !wasSynced
			}
		}
		var existing []model.RemoteOutboundConnection
		if err := tx.Where("subscription_id = ? AND missing = ?", subscription.Id, false).Find(&existing).Error; err != nil {
			return err
		}
		for _, connection := range existing {
			if _, ok := seen[connection.SourceKey]; ok {
				continue
			}
			connection.Missing = true
			connection.UpdatedAt = now
			if connection.Synced {
				if err := s.unsyncConnectionFromOutboundTx(tx, &connection); err != nil {
					return err
				}
				syncedChanged = true
			} else {
				if err := tx.Save(&connection).Error; err != nil {
					return err
				}
			}
			result.MarkedMissing++
		}
		if err := tx.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Updates(map[string]any{
			"tag_prefix":   subscription.TagPrefix,
			"last_updated": now,
			"last_error":   "",
			"updated_at":   now,
		}).Error; err != nil {
			return err
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "subscription_refresh", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, syncedChanged)
	return &result, nil
}

func (s *RemoteOutboundService) SyncConnectionToOutbound(id uint, loginUser string) (*model.RemoteOutboundConnection, error) {
	if id == 0 {
		return nil, common.NewError("connection id is required")
	}
	var saved model.RemoteOutboundConnection
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if _, err := s.reconcileOutboundLinksTx(tx); err != nil {
			return err
		}
		if err := reconcileRemoteOutboundGroupStatesTx(tx); err != nil {
			return err
		}
		if err := tx.First(&saved, id).Error; err != nil {
			return err
		}
		if err := s.syncConnectionToOutboundTx(tx, &saved, true); err != nil {
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

func (s *RemoteOutboundService) CheckConnection(ctx context.Context, id uint, target string) (*RemoteOutboundCheckResult, error) {
	var connection model.RemoteOutboundConnection
	if err := database.GetDB().First(&connection, id).Error; err != nil {
		return nil, err
	}
	return s.checkConnectionRecord(ctx, connection, target), nil
}

func (s *RemoteOutboundService) CheckSubscription(ctx context.Context, subscriptionID uint, target string) ([]RemoteOutboundCheckResult, error) {
	var connections []model.RemoteOutboundConnection
	if err := database.GetDB().
		Where("subscription_id = ?", subscriptionID).
		Order(sortOrderClause).
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return s.checkConnectionRecords(ctx, connections, target), nil
}

func (s *RemoteOutboundService) CheckAll(ctx context.Context, target string) ([]RemoteOutboundCheckResult, error) {
	var connections []model.RemoteOutboundConnection
	if err := database.GetDB().
		Where("enabled = ? AND missing = ?", true, false).
		Order("subscription_id ASC, sort_order ASC, id ASC").
		Find(&connections).Error; err != nil {
		return nil, err
	}
	return s.checkConnectionRecords(ctx, connections, target), nil
}

func (s *RemoteOutboundService) checkConnectionRecords(ctx context.Context, connections []model.RemoteOutboundConnection, target string) []RemoteOutboundCheckResult {
	results := make([]RemoteOutboundCheckResult, len(connections))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for i, connection := range connections {
		wg.Add(1)
		go func(index int, item model.RemoteOutboundConnection) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = RemoteOutboundCheckResult{
					ConnectionId: item.Id,
					OutboundTag:  item.OutboundTag,
					Error:        ctx.Err().Error(),
				}
				return
			}

			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			results[index] = *s.checkConnectionRecord(checkCtx, item, target)
		}(i, connection)
	}

	wg.Wait()
	return results
}

func (s *RemoteOutboundService) checkConnectionRecord(ctx context.Context, connection model.RemoteOutboundConnection, target string) *RemoteOutboundCheckResult {
	result := &RemoteOutboundCheckResult{
		ConnectionId: connection.Id,
		OutboundTag:  connection.OutboundTag,
	}
	switch {
	case !connection.Enabled:
		result.Skipped = true
		result.Error = "connection is disabled"
	case connection.Missing:
		result.Skipped = true
		result.Error = "connection is missing in latest subscription refresh"
	default:
		result.Result = checkRemoteConnectionWithTempCore(ctx, connection, target)
		result.Error = result.Result.Error
	}
	return result
}

func checkRemoteConnectionWithTempCore(ctx context.Context, connection model.RemoteOutboundConnection, target string) core.CheckOutboundResult {
	outbound, err := remoteConnectionOutboundConfig(connection)
	if err != nil {
		return core.CheckOutboundResult{Error: err.Error()}
	}
	config, err := json.Marshal(map[string]any{
		"log": map[string]any{
			"disabled": true,
		},
		"outbounds": []json.RawMessage{outbound},
	})
	if err != nil {
		return core.CheckOutboundResult{Error: err.Error()}
	}
	instance := core.NewCore()
	if err := instance.Start(config); err != nil {
		return core.CheckOutboundResult{Error: err.Error()}
	}
	defer func() {
		_ = instance.Stop()
	}()
	return instance.CheckOutbound(ctx, connection.OutboundTag, target)
}

func remoteConnectionOutboundConfig(connection model.RemoteOutboundConnection) (json.RawMessage, error) {
	if strings.TrimSpace(connection.Type) == "" {
		return nil, common.NewError("connection outbound type is empty")
	}
	if strings.TrimSpace(connection.OutboundTag) == "" {
		return nil, common.NewError("connection outbound tag is empty")
	}
	raw := map[string]any{}
	if len(connection.Options) > 0 {
		if err := json.Unmarshal(connection.Options, &raw); err != nil {
			return nil, err
		}
	}
	raw["type"] = connection.Type
	raw["tag"] = connection.OutboundTag
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func remoteConnectionOutboundMap(connection model.RemoteOutboundConnection) (map[string]interface{}, error) {
	rawConfig, err := remoteConnectionOutboundConfig(connection)
	if err != nil {
		return nil, err
	}
	outbound := map[string]interface{}{}
	if err := json.Unmarshal(rawConfig, &outbound); err != nil {
		return nil, err
	}
	return outbound, nil
}

func (s *RemoteOutboundService) OutboundsForClientLinks(rawLinks json.RawMessage) ([]map[string]interface{}, []string, error) {
	links := []remoteOutboundClientLink{}
	if len(rawLinks) == 0 {
		return nil, nil, nil
	}
	if err := json.Unmarshal(rawLinks, &links); err != nil {
		return nil, nil, nil
	}

	groupIDs := make([]uint, 0)
	seenGroups := map[uint]struct{}{}
	for _, link := range links {
		if link.Type != "remoteGroup" {
			continue
		}
		groupID := link.GroupId
		if groupID == 0 {
			groupID = link.RemoteGroupId
		}
		if groupID == 0 {
			continue
		}
		if _, ok := seenGroups[groupID]; ok {
			continue
		}
		seenGroups[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	if len(groupIDs) == 0 {
		return nil, nil, nil
	}

	var connections []model.RemoteOutboundConnection
	err := database.GetDB().
		Model(&model.RemoteOutboundConnection{}).
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").
		Joins("JOIN remote_outbound_groups ON remote_outbound_groups.id = remote_outbound_group_connections.group_id").
		Joins("JOIN remote_outbound_subscriptions ON remote_outbound_subscriptions.id = remote_outbound_connections.subscription_id").
		Where("remote_outbound_group_connections.group_id IN ?", groupIDs).
		Where("remote_outbound_groups.enabled = ? AND remote_outbound_subscriptions.enabled = ?", true, true).
		Where("remote_outbound_connections.enabled = ? AND remote_outbound_connections.missing = ?", true, false).
		Order("remote_outbound_groups.sort_order ASC, remote_outbound_groups.id ASC, remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").
		Find(&connections).Error
	if err != nil {
		return nil, nil, err
	}

	outbounds := make([]map[string]interface{}, 0, len(connections))
	tags := make([]string, 0, len(connections))
	seenConnections := map[uint]struct{}{}
	for _, connection := range connections {
		if _, ok := seenConnections[connection.Id]; ok {
			continue
		}
		seenConnections[connection.Id] = struct{}{}
		outbound, err := remoteConnectionOutboundMap(connection)
		if err != nil {
			return nil, nil, err
		}
		tag, _ := outbound["tag"].(string)
		if strings.TrimSpace(tag) == "" {
			continue
		}
		outbounds = append(outbounds, outbound)
		tags = append(tags, tag)
	}
	return outbounds, tags, nil
}

func (s *RemoteOutboundService) syncConnectionToOutboundTx(tx *gorm.DB, connection *model.RemoteOutboundConnection, requireUsable bool) error {
	if requireUsable {
		switch {
		case !connection.Enabled:
			return common.NewError("disabled connection can not be synced")
		case connection.Missing:
			return common.NewError("missing connection can not be synced")
		}
	}
	if strings.TrimSpace(connection.OutboundTag) == "" {
		return common.NewError("connection outbound tag is empty")
	}
	var outbound model.Outbound
	var err error
	if connection.OutboundId != nil && *connection.OutboundId != 0 {
		err = tx.First(&outbound, *connection.OutboundId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
	}
	if outbound.Id == 0 && err == nil {
		err = tx.Where("tag = ?", connection.OutboundTag).First(&outbound).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	if outbound.Id != 0 {
		if connection.OutboundId == nil || outbound.Id != *connection.OutboundId {
			var linked int64
			if err := tx.Model(&model.RemoteOutboundConnection{}).
				Where("outbound_id = ? AND id = ?", outbound.Id, connection.Id).
				Count(&linked).Error; err != nil {
				return err
			}
			if linked == 0 {
				return common.NewError("outbound tag already exists outside this remote connection")
			}
		}
		if connection.OutboundId != nil && outbound.Id != *connection.OutboundId {
			return common.NewError("outbound tag belongs to another outbound")
		}
		var linkedCount int64
		if err := tx.Model(&model.RemoteOutboundConnection{}).
			Where("outbound_id = ? AND id <> ?", outbound.Id, connection.Id).
			Count(&linkedCount).Error; err != nil {
			return err
		}
		if linkedCount > 0 {
			return common.NewError("outbound is already linked to another remote connection")
		}
	} else {
		sortOrder, err := nextSortOrder(tx, &model.Outbound{})
		if err != nil {
			return err
		}
		outbound.SortOrder = sortOrder
		outbound.Tag = connection.OutboundTag
	}
	outbound.Type = connection.Type
	outbound.Options = cloneRawMessage(connection.Options)
	if err := tx.Save(&outbound).Error; err != nil {
		return err
	}
	connection.OutboundId = &outbound.Id
	connection.Synced = true
	connection.UpdatedAt = time.Now().Unix()
	return nil
}

func (s *RemoteOutboundService) unsyncConnectionFromOutboundTx(tx *gorm.DB, connection *model.RemoteOutboundConnection) error {
	now := time.Now().Unix()
	if connection.OutboundId != nil && *connection.OutboundId != 0 {
		if err := removeOutboundReferencesTx(tx, connection.OutboundTag); err != nil {
			return err
		}
		if err := tx.
			Where("id = ? AND tag = ?", *connection.OutboundId, connection.OutboundTag).
			Delete(&model.Outbound{}).Error; err != nil {
			return err
		}
	}
	connection.Synced = false
	connection.OutboundId = nil
	connection.UpdatedAt = now
	return tx.Save(connection).Error
}

func (s *RemoteOutboundService) reconcileOutboundLinksTx(tx *gorm.DB) (bool, error) {
	var connections []model.RemoteOutboundConnection
	if err := tx.Where("synced = ?", true).Find(&connections).Error; err != nil {
		return false, err
	}
	changed := false
	now := time.Now().Unix()
	for _, connection := range connections {
		if connection.OutboundId == nil || *connection.OutboundId == 0 {
			if err := tx.Model(&model.RemoteOutboundConnection{}).
				Where("id = ?", connection.Id).
				Updates(map[string]any{
					"synced":      false,
					"outbound_id": nil,
					"updated_at":  now,
				}).Error; err != nil {
				return false, err
			}
			changed = true
			continue
		}
		var outbound model.Outbound
		err := tx.First(&outbound, *connection.OutboundId).Error
		if err == nil && outbound.Tag == connection.OutboundTag {
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
		if err := tx.Model(&model.RemoteOutboundConnection{}).
			Where("id = ?", connection.Id).
			Updates(map[string]any{
				"synced":      false,
				"outbound_id": nil,
				"updated_at":  now,
			}).Error; err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func (s *RemoteOutboundService) retagSubscriptionConnectionsTx(tx *gorm.DB, subscription *model.RemoteOutboundSubscription) (bool, error) {
	if subscription == nil || subscription.Id == 0 {
		return false, nil
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ?", subscription.Id).
		Order(sortOrderClause).
		Find(&connections).Error; err != nil {
		return false, err
	}
	coreRestart := false
	now := time.Now().Unix()
	for index := range connections {
		connection := &connections[index]
		newTag, err := s.uniqueRemoteOutboundTag(tx, *subscription, connection.Name, connection.Id)
		if err != nil {
			return false, err
		}
		if newTag == connection.OutboundTag {
			continue
		}
		oldTag := connection.OutboundTag
		if connection.Synced && connection.OutboundId != nil && *connection.OutboundId != 0 {
			result := tx.Model(&model.Outbound{}).
				Where("id = ? AND tag = ?", *connection.OutboundId, oldTag).
				Update("tag", newTag)
			if result.Error != nil {
				return false, result.Error
			}
			if result.RowsAffected > 0 {
				coreRestart = true
			} else {
				connection.Synced = false
				connection.OutboundId = nil
			}
		}
		connection.OutboundTag = newTag
		connection.UpdatedAt = now
		if err := tx.Save(connection).Error; err != nil {
			return false, err
		}
	}
	return coreRestart, nil
}

func remoteGroupConnections(tx *gorm.DB, groupID uint) ([]model.RemoteOutboundConnection, error) {
	var connections []model.RemoteOutboundConnection
	err := tx.
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").
		Where("remote_outbound_group_connections.group_id = ?", groupID).
		Order("remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").
		Find(&connections).Error
	return connections, err
}

func remoteOutboundGroupConnectionIDSetTx(tx *gorm.DB, groupID uint) (map[uint]struct{}, error) {
	var links []model.RemoteOutboundGroupConnection
	if err := tx.Where("group_id = ?", groupID).Find(&links).Error; err != nil {
		return nil, err
	}
	result := make(map[uint]struct{}, len(links))
	for _, link := range links {
		result[link.ConnectionId] = struct{}{}
	}
	return result, nil
}

func addRemoteOutboundGroupConnectionTx(tx *gorm.DB, groupID uint, connectionID uint, now int64) error {
	if groupID == 0 || connectionID == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.RemoteOutboundGroupConnection{}).
		Where("group_id = ? AND connection_id = ?", groupID, connectionID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return tx.Create(&model.RemoteOutboundGroupConnection{
		GroupId:      groupID,
		ConnectionId: connectionID,
		CreatedAt:    now,
	}).Error
}

func ensureRemoteOutboundConnectionHasGroupTx(tx *gorm.DB, connectionID uint, fallbackGroupID uint) error {
	var count int64
	if err := tx.Model(&model.RemoteOutboundGroupConnection{}).
		Where("connection_id = ?", connectionID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return addRemoteOutboundGroupConnectionTx(tx, fallbackGroupID, connectionID, time.Now().Unix())
}

func backfillRemoteOutboundGroupMembershipsTx(tx *gorm.DB) error {
	var subscriptions []model.RemoteOutboundSubscription
	if err := tx.Select("id").Find(&subscriptions).Error; err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if err := backfillRemoteOutboundSubscriptionGroupMembershipsTx(tx, subscription.Id); err != nil {
			return err
		}
	}
	return nil
}

func backfillRemoteOutboundSubscriptionGroupMembershipsTx(tx *gorm.DB, subscriptionID uint) error {
	now := time.Now().Unix()
	defaultGroup, err := ensureRemoteOutboundDefaultGroup(tx, subscriptionID, now)
	if err != nil {
		return err
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.Where("subscription_id = ?", subscriptionID).Find(&connections).Error; err != nil {
		return err
	}
	for _, connection := range connections {
		var membershipCount int64
		if err := tx.Model(&model.RemoteOutboundGroupConnection{}).
			Where("connection_id = ?", connection.Id).
			Count(&membershipCount).Error; err != nil {
			return err
		}
		if membershipCount > 0 || connection.GroupId == 0 {
			continue
		}

		groupID := connection.GroupId
		var count int64
		if err := tx.Model(&model.RemoteOutboundGroup{}).
			Where("id = ? AND subscription_id = ?", groupID, subscriptionID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			groupID = defaultGroup.Id
		}
		if err := ensureRemoteOutboundConnectionHasGroupTx(tx, connection.Id, groupID); err != nil {
			return err
		}
	}
	return syncRemoteOutboundLegacyGroupIDsTx(tx, subscriptionID)
}

func hydrateRemoteOutboundConnectionGroupIDs(tx *gorm.DB, subscriptions []model.RemoteOutboundSubscription) error {
	connectionRefs := make(map[uint]*model.RemoteOutboundConnection)
	connectionIDs := make([]uint, 0)
	for subIndex := range subscriptions {
		for connectionIndex := range subscriptions[subIndex].Connections {
			connection := &subscriptions[subIndex].Connections[connectionIndex]
			connection.GroupIds = nil
			connectionRefs[connection.Id] = connection
			connectionIDs = append(connectionIDs, connection.Id)
		}
	}
	if len(connectionIDs) == 0 {
		return nil
	}
	var links []model.RemoteOutboundGroupConnection
	if err := tx.
		Where("connection_id IN ?", connectionIDs).
		Order("group_id ASC").
		Find(&links).Error; err != nil {
		return err
	}
	for _, link := range links {
		if connection := connectionRefs[link.ConnectionId]; connection != nil {
			connection.GroupIds = append(connection.GroupIds, link.GroupId)
		}
	}
	return nil
}

func syncRemoteOutboundLegacyGroupIDsTx(tx *gorm.DB, subscriptionID uint) error {
	var connections []model.RemoteOutboundConnection
	if err := tx.Select("id").Where("subscription_id = ?", subscriptionID).Find(&connections).Error; err != nil {
		return err
	}
	for _, connection := range connections {
		if err := syncRemoteOutboundConnectionLegacyGroupIDTx(tx, connection.Id); err != nil {
			return err
		}
	}
	return nil
}

func syncRemoteOutboundConnectionLegacyGroupIDTx(tx *gorm.DB, connectionID uint) error {
	var group model.RemoteOutboundGroup
	err := tx.
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.group_id = remote_outbound_groups.id").
		Where("remote_outbound_group_connections.connection_id = ?", connectionID).
		Order("remote_outbound_groups.sort_order ASC, remote_outbound_groups.id ASC").
		First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Model(&model.RemoteOutboundConnection{}).
			Where("id = ?", connectionID).
			Update("group_id", 0).Error
	}
	if err != nil {
		return err
	}
	return tx.Model(&model.RemoteOutboundConnection{}).
		Where("id = ?", connectionID).
		Update("group_id", group.Id).Error
}

func remoteConnectionUsesOutboundEnabledGroupTx(tx *gorm.DB, connectionID uint) (bool, error) {
	var count int64
	if err := tx.Model(&model.RemoteOutboundGroup{}).
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.group_id = remote_outbound_groups.id").
		Where("remote_outbound_group_connections.connection_id = ? AND remote_outbound_groups.outbound_enabled = ?", connectionID, true).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *RemoteOutboundService) unsyncRemoteConnectionIfNoOutboundGroupTx(tx *gorm.DB, connectionID uint) (bool, error) {
	used, err := remoteConnectionUsesOutboundEnabledGroupTx(tx, connectionID)
	if err != nil || used {
		return false, err
	}
	var connection model.RemoteOutboundConnection
	if err := tx.First(&connection, connectionID).Error; err != nil {
		return false, err
	}
	if !connection.Synced {
		return false, nil
	}
	return true, s.unsyncConnectionFromOutboundTx(tx, &connection)
}

func reconcileRemoteOutboundGroupStatesTx(tx *gorm.DB) error {
	var groups []model.RemoteOutboundGroup
	if err := tx.Where("outbound_enabled = ?", true).Find(&groups).Error; err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, group := range groups {
		connections, err := remoteGroupConnections(tx, group.Id)
		if err != nil {
			return err
		}
		active := filterUsableRemoteConnections(connections)
		if len(active) == 0 || !remoteConnectionsAllSynced(active) {
			if err := tx.Model(&model.RemoteOutboundGroup{}).
				Where("id = ?", group.Id).
				Updates(map[string]any{"outbound_enabled": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func filterUsableRemoteConnections(connections []model.RemoteOutboundConnection) []model.RemoteOutboundConnection {
	active := make([]model.RemoteOutboundConnection, 0, len(connections))
	for _, connection := range connections {
		if connection.Enabled && !connection.Missing {
			active = append(active, connection)
		}
	}
	return active
}

func remoteConnectionsAllSynced(connections []model.RemoteOutboundConnection) bool {
	if len(connections) == 0 {
		return false
	}
	for _, connection := range connections {
		if !connection.Synced {
			return false
		}
	}
	return true
}

func mapKeys(values map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func (s *RemoteOutboundService) uniqueRemoteOutboundTag(tx *gorm.DB, subscription model.RemoteOutboundSubscription, name string, connectionID uint) (string, error) {
	prefix := sanitizeRemoteOutboundTagPrefix(subscription.TagPrefix)
	if prefix == "" {
		prefix = defaultRemoteOutboundTagPrefix(subscription.Name, subscription.Id)
	}
	base := prefix + sanitizeRemoteOutboundTagPart(name)
	if base == "" {
		base = prefix + "connection"
	}
	base = trimRemoteOutboundTag(base)
	for index := 0; index < 1000; index++ {
		candidate := base
		if index > 0 {
			candidate = trimRemoteOutboundTag(fmt.Sprintf("%s-%d", base, index+1))
		}
		available, err := remoteOutboundTagAvailable(tx, candidate, connectionID)
		if err != nil {
			return "", err
		}
		if available {
			return candidate, nil
		}
	}
	return "", common.NewError("unable to allocate unique outbound tag")
}

func remoteOutboundTagAvailable(tx *gorm.DB, tag string, connectionID uint) (bool, error) {
	var outbounds int64
	if err := tx.Model(&model.Outbound{}).Where("tag = ?", tag).Count(&outbounds).Error; err != nil {
		return false, err
	}
	if outbounds > 0 {
		if connectionID == 0 {
			return false, nil
		}
		var linked int64
		if err := tx.Model(&model.RemoteOutboundConnection{}).
			Where("outbound_tag = ? AND id = ?", tag, connectionID).
			Count(&linked).Error; err != nil {
			return false, err
		}
		return linked > 0, nil
	}
	var connections int64
	query := tx.Model(&model.RemoteOutboundConnection{}).Where("outbound_tag = ?", tag)
	if connectionID != 0 {
		query = query.Where("id <> ?", connectionID)
	}
	if err := query.Count(&connections).Error; err != nil {
		return false, err
	}
	return connections == 0, nil
}

func (s *RemoteOutboundService) recordRemoteOutboundChange(tx *gorm.DB, loginUser string, act string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return (&ConfigService{Runtime: s.runtime()}).recordConfigChange(tx, loginUser, "remoteOutboundSubscriptions", act, data)
}

func (s *RemoteOutboundService) invalidateRemoteOutboundData(loginUser string, coreRestart bool) {
	configService := s.configService()
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

type normalizedRemoteOutbound struct {
	Name      string
	Type      string
	SourceKey string
	Options   json.RawMessage
}

func normalizeRemoteOutbound(outbound map[string]interface{}, index int) (normalizedRemoteOutbound, error) {
	raw := cloneOutboundMap(outbound)
	outboundType, _ := raw["type"].(string)
	outboundType = strings.TrimSpace(outboundType)
	if outboundType == "" {
		return normalizedRemoteOutbound{}, common.NewError("subscription outbound has no type")
	}
	name := remoteOutboundDisplayName(raw, index)
	sourceKey, err := remoteOutboundSourceKey(raw)
	if err != nil {
		return normalizedRemoteOutbound{}, err
	}
	delete(raw, "id")
	delete(raw, "sortOrder")
	delete(raw, "sort_order")
	delete(raw, "type")
	delete(raw, "tag")
	options, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return normalizedRemoteOutbound{}, err
	}
	return normalizedRemoteOutbound{
		Name:      name,
		Type:      outboundType,
		SourceKey: sourceKey,
		Options:   options,
	}, nil
}

func updateRemoteOutboundConnection(connection *model.RemoteOutboundConnection, normalized normalizedRemoteOutbound, now int64) bool {
	changed := false
	if connection.Name == "" {
		connection.Name = normalized.Name
		changed = true
	}
	if connection.Type != normalized.Type {
		connection.Type = normalized.Type
		changed = true
	}
	if !jsonRawEqual(connection.Options, normalized.Options) {
		connection.Options = normalized.Options
		changed = true
	}
	if connection.Missing {
		connection.Missing = false
		changed = true
	}
	connection.LastSeen = now
	connection.UpdatedAt = now
	return changed
}

func remoteOutboundSourceKey(outbound map[string]interface{}) (string, error) {
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		if value, ok := outbound[key].(string); ok && strings.TrimSpace(value) != "" {
			label := strings.ToLower(strings.TrimSpace(value))
			outboundType, _ := outbound["type"].(string)
			return "label:" + strings.ToLower(strings.TrimSpace(outboundType)) + ":" + label, nil
		}
	}
	identity := cloneOutboundMap(outbound)
	delete(identity, "id")
	delete(identity, "sortOrder")
	delete(identity, "sort_order")
	delete(identity, "tag")
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func uniqueRemoteOutboundSourceKey(seen map[string]struct{}, sourceKey string) string {
	if _, ok := seen[sourceKey]; !ok {
		return sourceKey
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s:%d", sourceKey, index)
		if _, ok := seen[candidate]; !ok {
			return candidate
		}
	}
}

func remoteOutboundDisplayName(outbound map[string]interface{}, index int) string {
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		if value, ok := outbound[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	server, _ := outbound["server"].(string)
	port := remoteOutboundPortString(outbound["server_port"])
	if server != "" && port != "" {
		return server + ":" + port
	}
	outboundType, _ := outbound["type"].(string)
	outboundType = strings.TrimSpace(outboundType)
	if outboundType == "" {
		outboundType = "outbound"
	}
	return fmt.Sprintf("%s-%d", outboundType, index+1)
}

func remoteOutboundPortString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func cloneOutboundMap(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneRawMessage(input json.RawMessage) json.RawMessage {
	if input == nil {
		return nil
	}
	cloned := make([]byte, len(input))
	copy(cloned, input)
	return cloned
}

func jsonRawEqual(left json.RawMessage, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return string(left) == string(right)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return string(left) == string(right)
	}
	leftBytes, _ := json.Marshal(leftValue)
	rightBytes, _ := json.Marshal(rightValue)
	return string(leftBytes) == string(rightBytes)
}

func ensureRemoteOutboundDefaultGroup(tx *gorm.DB, subscriptionID uint, now int64) (*model.RemoteOutboundGroup, error) {
	var group model.RemoteOutboundGroup
	err := tx.Where("subscription_id = ? AND name = ?", subscriptionID, defaultRemoteOutboundGroupName).First(&group).Error
	if err == nil {
		return &group, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	sortOrder, err := nextSortOrder(tx, &model.RemoteOutboundGroup{})
	if err != nil {
		return nil, err
	}
	group = model.RemoteOutboundGroup{
		SubscriptionId: subscriptionID,
		SortOrder:      sortOrder,
		Name:           defaultRemoteOutboundGroupName,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tx.Create(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func defaultRemoteOutboundTagPrefix(name string, id uint) string {
	base := sanitizeRemoteOutboundTagPart(name)
	if base == "" {
		base = "remote"
	}
	if id != 0 {
		return fmt.Sprintf("ros%d-%s-", id, base)
	}
	return "ros{id}-" + base + "-"
}

func sanitizeRemoteOutboundTagPrefix(value string) string {
	value = sanitizeRemoteOutboundTagText(value)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, "-_. ")
	if value == "" {
		return ""
	}
	if !strings.HasSuffix(value, "-") && !strings.HasSuffix(value, "_") && !strings.HasSuffix(value, ".") {
		value += "-"
	}
	return trimRemoteOutboundTag(value)
}

func sanitizeRemoteOutboundTagPart(value string) string {
	value = strings.Trim(sanitizeRemoteOutboundTagText(value), "-_. ")
	if value == "" {
		return "connection"
	}
	return value
}

func sanitizeRemoteOutboundTagText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastWasDash := false
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			continue
		case unicode.IsSpace(r):
			if !lastWasDash {
				builder.WriteRune('-')
				lastWasDash = true
			}
		default:
			builder.WriteRune(r)
			lastWasDash = r == '-'
		}
	}
	return strings.Trim(builder.String(), "-")
}

func trimRemoteOutboundTag(value string) string {
	const maxTagLength = 96
	value = strings.TrimFunc(value, unicode.IsSpace)
	runes := []rune(value)
	if len(runes) <= maxTagLength {
		return value
	}
	return strings.TrimRightFunc(string(runes[:maxTagLength]), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
}

func normalizeRemoteOutboundUpdateInterval(value int64) int64 {
	const defaultInterval = int64(24 * 60 * 60)
	const minInterval = int64(5 * 60)
	if value <= 0 {
		return defaultInterval
	}
	if value < minInterval {
		return minInterval
	}
	return value
}
