//go:build !minimal

package remote

import (
	"errors"
	"time"
)

const DefaultGroupName = "Default"

func GroupConnections(tx Store, groupID uint) ([]RemoteOutboundConnection, error) {
	var connections []RemoteOutboundConnection
	err := tx.ListGroupConnections(groupID, &connections)
	return connections, err
}

func GroupConnectionIDSet(tx Store, groupID uint) (map[uint]struct{}, error) {
	var links []RemoteOutboundGroupConnection
	if err := tx.ListGroupMemberships(groupID, &links); err != nil {
		return nil, err
	}
	result := make(map[uint]struct{}, len(links))
	for _, link := range links {
		result[link.ConnectionId] = struct{}{}
	}
	return result, nil
}

func AddGroupConnection(tx Store, groupID uint, connectionID uint, now int64) error {
	if groupID == 0 || connectionID == 0 {
		return nil
	}
	var count int64
	if err := tx.CountMembership(groupID, connectionID, &count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return tx.CreateMembership(&RemoteOutboundGroupConnection{GroupId: groupID, ConnectionId: connectionID, CreatedAt: now})
}

func EnsureConnectionHasGroup(tx Store, connectionID uint, fallbackGroupID uint) error {
	var count int64
	if err := tx.CountConnectionMemberships(connectionID, &count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return AddGroupConnection(tx, fallbackGroupID, connectionID, time.Now().Unix())
}

func BackfillGroupMemberships(tx Store) error {
	var subscriptions []RemoteOutboundSubscription
	if err := tx.ListSubscriptionIDs(&subscriptions); err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if err := BackfillSubscriptionGroupMemberships(tx, subscription.Id); err != nil {
			return err
		}
	}
	return nil
}

func BackfillSubscriptionGroupMemberships(tx Store, subscriptionID uint) error {
	now := time.Now().Unix()
	defaultGroup, err := EnsureDefaultGroup(tx, subscriptionID, now)
	if err != nil {
		return err
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListSubscriptionConnections(subscriptionID, &connections); err != nil {
		return err
	}
	for _, connection := range connections {
		var membershipCount int64
		if err := tx.CountConnectionMemberships(connection.Id, &membershipCount); err != nil {
			return err
		}
		if membershipCount > 0 || connection.GroupId == 0 {
			continue
		}

		groupID := connection.GroupId
		var count int64
		if err := tx.CountSubscriptionGroup(subscriptionID, groupID, &count); err != nil {
			return err
		}
		if count == 0 {
			groupID = defaultGroup.Id
		}
		if err := EnsureConnectionHasGroup(tx, connection.Id, groupID); err != nil {
			return err
		}
	}
	if err := SyncLegacyGroupIDs(tx, subscriptionID); err != nil {
		return err
	}
	return ReconcileDerivedGroupDependencies(tx, subscriptionID, defaultGroup.Id)
}

func HydrateConnectionGroupIDs(tx Store, subscriptions []RemoteOutboundSubscription) error {
	connectionRefs := make(map[uint]*RemoteOutboundConnection)
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
	var links []RemoteOutboundGroupConnection
	if err := tx.ListMembershipsForConnections(connectionIDs, &links); err != nil {
		return err
	}
	for _, link := range links {
		if connection := connectionRefs[link.ConnectionId]; connection != nil {
			connection.GroupIds = append(connection.GroupIds, link.GroupId)
		}
	}
	return nil
}

func SyncLegacyGroupIDs(tx Store, subscriptionID uint) error {
	var connections []RemoteOutboundConnection
	if err := tx.ListSubscriptionConnectionIDs(subscriptionID, &connections); err != nil {
		return err
	}
	for _, connection := range connections {
		if err := SyncConnectionLegacyGroupID(tx, connection.Id); err != nil {
			return err
		}
	}
	return nil
}

func SyncConnectionLegacyGroupID(tx Store, connectionID uint) error {
	var group RemoteOutboundGroup
	err := tx.FirstConnectionGroup(connectionID, &group)
	if errors.Is(err, ErrRecordNotFound) {
		return tx.SetLegacyGroup(connectionID, 0)
	}
	if err != nil {
		return err
	}
	return tx.SetLegacyGroup(connectionID, group.Id)
}

func ConnectionUsesOutboundEnabledGroup(tx Store, connectionID uint) (bool, error) {
	var count int64
	if err := tx.CountOutboundGroupsForConnection(connectionID, &count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func ReconcileGroupStates(tx Store) error {
	var groups []RemoteOutboundGroup
	if err := tx.ListOutboundEnabledGroups(&groups); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, group := range groups {
		connections, err := GroupConnections(tx, group.Id)
		if err != nil {
			return err
		}
		active := FilterUsableConnections(connections)
		if len(active) == 0 || !ConnectionsAllSynced(active) {
			if err := tx.DisableGroupOutbounds(group.Id, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func FilterUsableConnections(connections []RemoteOutboundConnection) []RemoteOutboundConnection {
	active := make([]RemoteOutboundConnection, 0, len(connections))
	for _, connection := range connections {
		if connection.Enabled {
			active = append(active, connection)
		}
	}
	return active
}

func ConnectionsAllSynced(connections []RemoteOutboundConnection) bool {
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

func MapKeys(values map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func EnsureDefaultGroup(tx Store, subscriptionID uint, now int64) (*RemoteOutboundGroup, error) {
	var group RemoteOutboundGroup
	err := tx.FindDefaultGroup(subscriptionID, &group)
	if err == nil {
		return &group, nil
	}
	if !errors.Is(err, ErrRecordNotFound) {
		return nil, err
	}
	sortOrder, err := tx.NextGroupOrder()
	if err != nil {
		return nil, err
	}
	group = RemoteOutboundGroup{
		SubscriptionId: subscriptionID,
		SortOrder:      sortOrder,
		Name:           DefaultGroupName,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tx.CreateGroup(&group); err != nil {
		return nil, err
	}
	return &group, nil
}
