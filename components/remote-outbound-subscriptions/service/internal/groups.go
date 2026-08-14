//go:build !minimal

package remotesubservice

import (
	"strings"
	"time"

	remotesub "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/domain"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type BulkGroupResult struct {
	Name    string `json:"name"`
	Created int    `json:"created"`
	Skipped int    `json:"skipped"`
}

func (s *Service) SaveGroup(input remotesub.RemoteOutboundGroup, enabledProvided bool, loginUser string) (*remotesub.RemoteOutboundGroup, error) {
	now := time.Now().Unix()
	input.Name = strings.TrimSpace(input.Name)
	if input.SubscriptionId == 0 {
		return nil, common.NewError("subscription id is required")
	}
	if input.Name == "" {
		return nil, common.NewError("group name can not be empty")
	}
	if len([]rune(input.Name)) > 120 {
		return nil, common.NewError("group name is too long")
	}
	if !enabledProvided && input.Id == 0 {
		input.Enabled = true
	}
	var saved remotesub.RemoteOutboundGroup
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&remotesub.RemoteOutboundSubscription{}).Where("id = ?", input.SubscriptionId).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return common.NewError("subscription not found")
		}
		sortOrder, err := entityorder.ForSave(tx, &remotesub.RemoteOutboundGroup{}, input.Id)
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
			saved = remotesub.RemoteOutboundGroup{
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

func (s *Service) SaveGroupForAllSubscriptions(name string, loginUser string) (*BulkGroupResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.NewError("group name can not be empty")
	}
	if len([]rune(name)) > 120 {
		return nil, common.NewError("group name is too long")
	}
	now := time.Now().Unix()
	result := BulkGroupResult{Name: name}
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		var subscriptions []remotesub.RemoteOutboundSubscription
		if err := tx.Order(entityorder.Clause).Find(&subscriptions).Error; err != nil {
			return err
		}
		if len(subscriptions) == 0 {
			return common.NewError("no remote subscriptions")
		}
		for _, subscription := range subscriptions {
			var existing int64
			if err := tx.Model(&remotesub.RemoteOutboundGroup{}).
				Where("subscription_id = ? AND name = ?", subscription.Id, name).
				Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				result.Skipped++
				continue
			}
			sortOrder, err := nextGroupSortOrder(tx, subscription.Id)
			if err != nil {
				return err
			}
			group := remotesub.RemoteOutboundGroup{
				SubscriptionId: subscription.Id,
				SortOrder:      sortOrder,
				Name:           name,
				Enabled:        true,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if err := tx.Create(&group).Error; err != nil {
				return err
			}
			result.Created++
		}
		return s.recordRemoteOutboundChange(tx, loginUser, "groups_bulk_save", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return &result, nil
}

func nextGroupSortOrder(tx *gorm.DB, subscriptionID uint) (int, error) {
	var maxSortOrder int
	if err := tx.Model(&remotesub.RemoteOutboundGroup{}).
		Where("subscription_id = ?", subscriptionID).
		Select("COALESCE(MAX(sort_order), 0)").
		Scan(&maxSortOrder).Error; err != nil {
		return 0, err
	}
	return maxSortOrder + 1, nil
}

func (s *Service) DeleteGroup(id uint, loginUser string) error {
	if id == 0 {
		return common.NewError("group id is required")
	}
	var group remotesub.RemoteOutboundGroup
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		deleted, err := remotesub.DeleteGroup(tx, id, time.Now().Unix())
		if err != nil {
			return err
		}
		group = deleted
		if err := s.recordRemoteOutboundChange(tx, loginUser, "group_delete", group); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return nil
}
func (s *Service) MoveConnectionToGroup(connectionID uint, groupID uint, loginUser string) error {
	if connectionID == 0 || groupID == 0 {
		return common.NewError("connection id and group id are required")
	}
	var connection remotesub.RemoteOutboundConnection
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		moved, err := remotesub.MoveConnectionToGroup(tx, connectionID, groupID, time.Now().Unix())
		if err != nil {
			return err
		}
		connection = moved
		return s.recordRemoteOutboundChange(tx, loginUser, "connection_group", connection)
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, false)
	return nil
}
func (s *Service) SetGroupConnections(groupID uint, connectionIDs []uint, loginUser string) error {
	if groupID == 0 {
		return common.NewError("group id is required")
	}
	if len(connectionIDs) > remotesub.MaxFetchedOutbounds {
		return common.NewError("too many group connections")
	}
	coreRestart := false
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		changed, err := remotesub.SetGroupConnections(tx, groupID, connectionIDs, time.Now().Unix())
		if err != nil {
			return err
		}
		coreRestart = changed
		return s.recordRemoteOutboundChange(tx, loginUser, "group_connections", map[string]any{
			"groupId":       groupID,
			"connectionIds": connectionIDs,
		})
	})
	if err != nil {
		return err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return nil
}
func (s *Service) ToggleGroupOutbounds(groupID uint, loginUser string) (*remotesub.GroupActionResult, error) {
	if groupID == 0 {
		return nil, common.NewError("group id is required")
	}
	var result *remotesub.GroupActionResult
	coreRestart := false
	err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		actionResult, changed, err := remotesub.ToggleGroupOutbounds(tx, groupID, time.Now().Unix())
		if err != nil {
			return err
		}
		result = actionResult
		coreRestart = changed
		return s.recordRemoteOutboundChange(tx, loginUser, "group_outbounds", result)
	})
	if err != nil {
		return nil, err
	}
	s.invalidateRemoteOutboundData(loginUser, coreRestart)
	return result, nil
}
