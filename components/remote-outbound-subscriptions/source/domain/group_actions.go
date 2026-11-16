//go:build !minimal

package remote

import ()

type GroupActionResult struct {
	GroupId    uint `json:"groupId"`
	Added      int  `json:"added"`
	Removed    int  `json:"removed"`
	Skipped    int  `json:"skipped"`
	OutboundOn bool `json:"outboundOn"`
}

func DeleteGroup(tx Store, id uint, now int64) (RemoteOutboundGroup, error) {
	var group RemoteOutboundGroup
	if err := tx.LoadGroup(id, &group); err != nil {
		return group, err
	}
	defaultGroup, err := EnsureDefaultGroup(tx, group.SubscriptionId, now)
	if err != nil {
		return group, err
	}
	if group.Id == defaultGroup.Id {
		return group, sourceError("default group can not be deleted")
	}
	var affected []RemoteOutboundGroupConnection
	if err := tx.ListGroupMemberships(group.Id, &affected); err != nil {
		return group, err
	}
	if err := tx.DeleteGroupMemberships(group.Id); err != nil {
		return group, err
	}
	for _, link := range affected {
		if err := EnsureConnectionHasGroup(tx, link.ConnectionId, defaultGroup.Id); err != nil {
			return group, err
		}
	}
	return group, tx.DeleteGroup(group.Id)
}

func MoveConnectionToGroup(tx Store, connectionID uint, groupID uint, now int64) (RemoteOutboundConnection, error) {
	var connection RemoteOutboundConnection
	if err := tx.LoadConnection(connectionID, &connection); err != nil {
		return connection, err
	}
	var group RemoteOutboundGroup
	if err := tx.LoadGroup(groupID, &group); err != nil {
		return connection, err
	}
	if group.SubscriptionId != connection.SubscriptionId {
		return connection, sourceError("group does not belong to connection subscription")
	}
	connection.GroupId = group.Id
	connection.UpdatedAt = now
	if err := tx.SaveConnection(&connection); err != nil {
		return connection, err
	}
	if err := tx.DeleteConnectionMemberships(connection.Id); err != nil {
		return connection, err
	}
	if err := AddGroupConnection(tx, group.Id, connection.Id, now); err != nil {
		return connection, err
	}
	return connection, nil
}

func SetGroupConnections(tx Store, groupID uint, connectionIDs []uint, now int64) (bool, error) {
	var group RemoteOutboundGroup
	if err := tx.LoadGroup(groupID, &group); err != nil {
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

func ToggleGroupOutbounds(tx Store, groupID uint, now int64) (*GroupActionResult, bool, error) {
	result := &GroupActionResult{GroupId: groupID}
	if _, err := ReconcileOutboundLinks(tx); err != nil {
		return nil, false, err
	}
	if err := ReconcileGroupStates(tx); err != nil {
		return nil, false, err
	}
	var group RemoteOutboundGroup
	if err := tx.LoadGroup(groupID, &group); err != nil {
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
		return nil, false, sourceError("group has no usable connections")
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

func UnsyncConnectionIfNoOutboundGroup(tx Store, connectionID uint) (bool, error) {
	used, err := ConnectionUsesOutboundEnabledGroup(tx, connectionID)
	if err != nil || used {
		return false, err
	}
	var connection RemoteOutboundConnection
	if err := tx.LoadConnection(connectionID, &connection); err != nil {
		return false, err
	}
	if !connection.Synced {
		return false, nil
	}
	return true, UnsyncConnectionFromOutbound(tx, &connection)
}

func selectedConnectionSet(tx Store, subscriptionID uint, connectionIDs []uint) (map[uint]struct{}, error) {
	selected := make(map[uint]struct{}, len(connectionIDs))
	for _, id := range connectionIDs {
		if id == 0 {
			return nil, sourceError("connection id can not be empty")
		}
		selected[id] = struct{}{}
	}
	if len(selected) == 0 {
		return selected, nil
	}
	var count int64
	if err := tx.CountSelectedConnections(subscriptionID, MapKeys(selected), &count); err != nil {
		return nil, err
	}
	if count != int64(len(selected)) {
		return nil, sourceError("some connections do not belong to this subscription")
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

func removeGroupConnections(tx Store, group RemoteOutboundGroup, defaultGroupID uint, removed []uint) (bool, error) {
	if err := tx.DeleteSelectedMemberships(group.Id, removed); err != nil {
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

func addGroupConnections(tx Store, group RemoteOutboundGroup, added []uint, now int64) (bool, error) {
	coreRestart := false
	for _, connectionID := range added {
		if err := AddGroupConnection(tx, group.Id, connectionID, now); err != nil {
			return false, err
		}
		if group.OutboundEnabled {
			var connection RemoteOutboundConnection
			if err := tx.LoadConnection(connectionID, &connection); err != nil {
				return false, err
			}
			if connection.Enabled {
				wasSynced := connection.Synced
				if err := SyncConnectionToOutbound(tx, &connection, true); err != nil {
					return false, err
				}
				if err := tx.SaveConnection(&connection); err != nil {
					return false, err
				}
				coreRestart = coreRestart || !wasSynced
			}
		}
	}
	return coreRestart, nil
}

func disableGroupOutbounds(tx Store, group *RemoteOutboundGroup, connections []RemoteOutboundConnection, now int64, result *GroupActionResult) error {
	group.OutboundEnabled = false
	group.UpdatedAt = now
	if err := tx.SaveGroup(group); err != nil {
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
			if ConnectionIsGroup(connections[index]) {
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

func enableGroupOutbounds(tx Store, group *RemoteOutboundGroup, active []RemoteOutboundConnection, now int64, result *GroupActionResult) error {
	group.OutboundEnabled = true
	group.UpdatedAt = now
	if err := tx.SaveGroup(group); err != nil {
		return err
	}
	for index := range active {
		wasSynced := active[index].Synced
		if err := SyncConnectionToOutbound(tx, &active[index], true); err != nil {
			return err
		}
		if err := tx.SaveConnection(&active[index]); err != nil {
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
