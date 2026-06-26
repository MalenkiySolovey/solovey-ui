package remote

import (
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"gorm.io/gorm"
)

const DefaultGroupName = "Default"

func GroupConnections(tx *gorm.DB, groupID uint) ([]model.RemoteOutboundConnection, error) {
	var connections []model.RemoteOutboundConnection
	err := tx.
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").
		Where("remote_outbound_group_connections.group_id = ?", groupID).
		Order("remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").
		Find(&connections).Error
	return connections, err
}

func GroupConnectionIDSet(tx *gorm.DB, groupID uint) (map[uint]struct{}, error) {
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

func AddGroupConnection(tx *gorm.DB, groupID uint, connectionID uint, now int64) error {
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

func EnsureConnectionHasGroup(tx *gorm.DB, connectionID uint, fallbackGroupID uint) error {
	var count int64
	if err := tx.Model(&model.RemoteOutboundGroupConnection{}).
		Where("connection_id = ?", connectionID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return AddGroupConnection(tx, fallbackGroupID, connectionID, time.Now().Unix())
}

func BackfillGroupMemberships(tx *gorm.DB) error {
	var subscriptions []model.RemoteOutboundSubscription
	if err := tx.Select("id").Find(&subscriptions).Error; err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if err := BackfillSubscriptionGroupMemberships(tx, subscription.Id); err != nil {
			return err
		}
	}
	return nil
}

func BackfillSubscriptionGroupMemberships(tx *gorm.DB, subscriptionID uint) error {
	now := time.Now().Unix()
	defaultGroup, err := EnsureDefaultGroup(tx, subscriptionID, now)
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
		if err := EnsureConnectionHasGroup(tx, connection.Id, groupID); err != nil {
			return err
		}
	}
	if err := SyncLegacyGroupIDs(tx, subscriptionID); err != nil {
		return err
	}
	return ReconcileDerivedGroupDependencies(tx, subscriptionID, defaultGroup.Id)
}

func HydrateConnectionGroupIDs(tx *gorm.DB, subscriptions []model.RemoteOutboundSubscription) error {
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

func SyncLegacyGroupIDs(tx *gorm.DB, subscriptionID uint) error {
	var connections []model.RemoteOutboundConnection
	if err := tx.Select("id").Where("subscription_id = ?", subscriptionID).Find(&connections).Error; err != nil {
		return err
	}
	for _, connection := range connections {
		if err := SyncConnectionLegacyGroupID(tx, connection.Id); err != nil {
			return err
		}
	}
	return nil
}

func SyncConnectionLegacyGroupID(tx *gorm.DB, connectionID uint) error {
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

func ConnectionUsesOutboundEnabledGroup(tx *gorm.DB, connectionID uint) (bool, error) {
	var count int64
	if err := tx.Model(&model.RemoteOutboundGroup{}).
		Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.group_id = remote_outbound_groups.id").
		Where("remote_outbound_group_connections.connection_id = ? AND remote_outbound_groups.outbound_enabled = ?", connectionID, true).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func ReconcileGroupStates(tx *gorm.DB) error {
	var groups []model.RemoteOutboundGroup
	if err := tx.Where("outbound_enabled = ?", true).Find(&groups).Error; err != nil {
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
			if err := tx.Model(&model.RemoteOutboundGroup{}).
				Where("id = ?", group.Id).
				Updates(map[string]any{"outbound_enabled": false, "updated_at": now}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func FilterUsableConnections(connections []model.RemoteOutboundConnection) []model.RemoteOutboundConnection {
	active := make([]model.RemoteOutboundConnection, 0, len(connections))
	for _, connection := range connections {
		if connection.Enabled && !connection.Missing {
			active = append(active, connection)
		}
	}
	return active
}

func ConnectionsAllSynced(connections []model.RemoteOutboundConnection) bool {
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

func EnsureDefaultGroup(tx *gorm.DB, subscriptionID uint, now int64) (*model.RemoteOutboundGroup, error) {
	var group model.RemoteOutboundGroup
	err := tx.Where("subscription_id = ? AND name = ?", subscriptionID, DefaultGroupName).First(&group).Error
	if err == nil {
		return &group, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	sortOrder, err := entityorder.Next(tx, &model.RemoteOutboundGroup{})
	if err != nil {
		return nil, err
	}
	group = model.RemoteOutboundGroup{
		SubscriptionId: subscriptionID,
		SortOrder:      sortOrder,
		Name:           DefaultGroupName,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tx.Create(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}
