package remote

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type GroupActionResult struct {
	GroupId    uint `json:"groupId"`
	Added      int  `json:"added"`
	Removed    int  `json:"removed"`
	Skipped    int  `json:"skipped"`
	OutboundOn bool `json:"outboundOn"`
}

func DeleteGroup(tx *gorm.DB, id uint, now int64) (model.RemoteOutboundGroup, error) {
	var group model.RemoteOutboundGroup
	if err := tx.First(&group, id).Error; err != nil {
		return group, err
	}
	defaultGroup, err := EnsureDefaultGroup(tx, group.SubscriptionId, now)
	if err != nil {
		return group, err
	}
	if group.Id == defaultGroup.Id {
		return group, common.NewError("default group can not be deleted")
	}
	var affected []model.RemoteOutboundGroupConnection
	if err := tx.Where("group_id = ?", group.Id).Find(&affected).Error; err != nil {
		return group, err
	}
	if err := tx.Where("group_id = ?", group.Id).Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
		return group, err
	}
	for _, link := range affected {
		if err := EnsureConnectionHasGroup(tx, link.ConnectionId, defaultGroup.Id); err != nil {
			return group, err
		}
	}
	return group, tx.Delete(&group).Error
}

func MoveConnectionToGroup(tx *gorm.DB, connectionID uint, groupID uint, now int64) (model.RemoteOutboundConnection, error) {
	var connection model.RemoteOutboundConnection
	if err := tx.First(&connection, connectionID).Error; err != nil {
		return connection, err
	}
	var group model.RemoteOutboundGroup
	if err := tx.First(&group, groupID).Error; err != nil {
		return connection, err
	}
	if group.SubscriptionId != connection.SubscriptionId {
		return connection, common.NewError("group does not belong to connection subscription")
	}
	connection.GroupId = group.Id
	connection.UpdatedAt = now
	if err := tx.Save(&connection).Error; err != nil {
		return connection, err
	}
	if err := tx.Where("connection_id = ?", connection.Id).Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
		return connection, err
	}
	if err := AddGroupConnection(tx, group.Id, connection.Id, now); err != nil {
		return connection, err
	}
	return connection, nil
}

func SetGroupConnections(tx *gorm.DB, groupID uint, connectionIDs []uint, now int64) (bool, error) {
	var group model.RemoteOutboundGroup
	if err := tx.First(&group, groupID).Error; err != nil {
		return false, err
	}
	defaultGroup, err := EnsureDefaultGroup(tx, group.SubscriptionId, now)
	if err != nil {
		return false, err
	}
	selected, err := selectedConnectionSet(tx, group.SubscriptionId, connectionIDs)
	if err != nil {
		return false, err
	}
	current, err := GroupConnectionIDSet(tx, group.Id)
	if err != nil {
		return false, err
	}
	removed, added := diffConnectionSets(current, selected)

	coreRestart := false
	if len(removed) > 0 {
		changed, err := removeGroupConnections(tx, group, defaultGroup.Id, removed)
		if err != nil {
			return false, err
		}
		coreRestart = coreRestart || changed
	}
	if len(added) > 0 {
		changed, err := addGroupConnections(tx, group, added, now)
		if err != nil {
			return false, err
		}
		coreRestart = coreRestart || changed
	}
	if err := SyncLegacyGroupIDs(tx, group.SubscriptionId); err != nil {
		return false, err
	}
	if group.Id == defaultGroup.Id {
		if err := ReconcileDerivedGroupDependencies(tx, group.SubscriptionId, defaultGroup.Id); err != nil {
			return false, err
		}
	}
	return coreRestart, nil
}

func ToggleGroupOutbounds(tx *gorm.DB, groupID uint, now int64) (*GroupActionResult, bool, error) {
	result := &GroupActionResult{GroupId: groupID}
	if _, err := ReconcileOutboundLinks(tx); err != nil {
		return nil, false, err
	}
	if err := ReconcileGroupStates(tx); err != nil {
		return nil, false, err
	}
	var group model.RemoteOutboundGroup
	if err := tx.First(&group, groupID).Error; err != nil {
		return nil, false, err
	}
	defaultGroup, err := EnsureDefaultGroup(tx, group.SubscriptionId, now)
	if err != nil {
		return nil, false, err
	}
	if group.Id == defaultGroup.Id {
		if err := ReconcileDerivedGroupDependencies(tx, group.SubscriptionId, defaultGroup.Id); err != nil {
			return nil, false, err
		}
	}
	connections, err := GroupConnections(tx, group.Id)
	if err != nil {
		return nil, false, err
	}
	active := FilterUsableConnections(connections)
	enableOutbounds := !group.OutboundEnabled
	if enableOutbounds && len(active) == 0 {
		return nil, false, common.NewError("group has no usable connections")
	}
	if !enableOutbounds {
		if err := disableGroupOutbounds(tx, &group, connections, now, result); err != nil {
			return nil, false, err
		}
	} else if err := enableGroupOutbounds(tx, &group, active, now, result); err != nil {
		return nil, false, err
	}
	return result, result.Added > 0 || result.Removed > 0, nil
}

func UnsyncConnectionIfNoOutboundGroup(tx *gorm.DB, connectionID uint) (bool, error) {
	used, err := ConnectionUsesOutboundEnabledGroup(tx, connectionID)
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
	return true, UnsyncConnectionFromOutbound(tx, &connection)
}

func selectedConnectionSet(tx *gorm.DB, subscriptionID uint, connectionIDs []uint) (map[uint]struct{}, error) {
	selected := make(map[uint]struct{}, len(connectionIDs))
	for _, id := range connectionIDs {
		if id == 0 {
			return nil, common.NewError("connection id can not be empty")
		}
		selected[id] = struct{}{}
	}
	if len(selected) == 0 {
		return selected, nil
	}
	var count int64
	if err := tx.Model(&model.RemoteOutboundConnection{}).
		Where("subscription_id = ? AND id IN ?", subscriptionID, MapKeys(selected)).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if count != int64(len(selected)) {
		return nil, common.NewError("some connections do not belong to this subscription")
	}
	return selected, nil
}

func diffConnectionSets(current map[uint]struct{}, selected map[uint]struct{}) ([]uint, []uint) {
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
	return removed, added
}

func removeGroupConnections(tx *gorm.DB, group model.RemoteOutboundGroup, defaultGroupID uint, removed []uint) (bool, error) {
	if err := tx.
		Where("group_id = ? AND connection_id IN ?", group.Id, removed).
		Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
		return false, err
	}
	isDefaultGroup := group.Id == defaultGroupID
	coreRestart := false
	for _, connectionID := range removed {
		if !isDefaultGroup {
			if err := EnsureConnectionHasGroup(tx, connectionID, defaultGroupID); err != nil {
				return false, err
			}
		}
		if err := SyncConnectionLegacyGroupID(tx, connectionID); err != nil {
			return false, err
		}
		if group.OutboundEnabled {
			changed, err := UnsyncConnectionIfNoOutboundGroup(tx, connectionID)
			if err != nil {
				return false, err
			}
			coreRestart = coreRestart || changed
		}
	}
	return coreRestart, nil
}

func addGroupConnections(tx *gorm.DB, group model.RemoteOutboundGroup, added []uint, now int64) (bool, error) {
	coreRestart := false
	for _, connectionID := range added {
		if err := AddGroupConnection(tx, group.Id, connectionID, now); err != nil {
			return false, err
		}
		if group.OutboundEnabled {
			var connection model.RemoteOutboundConnection
			if err := tx.First(&connection, connectionID).Error; err != nil {
				return false, err
			}
			if connection.Enabled && !connection.Missing {
				wasSynced := connection.Synced
				if err := SyncConnectionToOutbound(tx, &connection, true); err != nil {
					return false, err
				}
				if err := tx.Save(&connection).Error; err != nil {
					return false, err
				}
				coreRestart = coreRestart || !wasSynced
			}
		}
	}
	return coreRestart, nil
}

func disableGroupOutbounds(tx *gorm.DB, group *model.RemoteOutboundGroup, connections []model.RemoteOutboundConnection, now int64, result *GroupActionResult) error {
	group.OutboundEnabled = false
	group.UpdatedAt = now
	if err := tx.Save(group).Error; err != nil {
		return err
	}
	for index := range connections {
		if !connections[index].Synced {
			result.Skipped++
			continue
		}
		changed, err := UnsyncConnectionIfNoOutboundGroup(tx, connections[index].Id)
		if err != nil {
			return err
		}
		if changed {
			result.Removed++
			if remoteConnectionIsGroup(connections[index]) {
				removed, err := unsyncGroupDependenciesIfUnused(tx, connections[index])
				if err != nil {
					return err
				}
				result.Removed += removed
			}
		} else {
			result.Skipped++
		}
	}
	result.OutboundOn = false
	return nil
}

func enableGroupOutbounds(tx *gorm.DB, group *model.RemoteOutboundGroup, active []model.RemoteOutboundConnection, now int64, result *GroupActionResult) error {
	group.OutboundEnabled = true
	group.UpdatedAt = now
	if err := tx.Save(group).Error; err != nil {
		return err
	}
	for index := range active {
		wasSynced := active[index].Synced
		if err := SyncConnectionToOutbound(tx, &active[index], true); err != nil {
			return err
		}
		if err := tx.Save(&active[index]).Error; err != nil {
			return err
		}
		if wasSynced {
			result.Skipped++
			continue
		}
		result.Added++
	}
	result.OutboundOn = true
	return nil
}
