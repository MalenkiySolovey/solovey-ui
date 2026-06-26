package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"gorm.io/gorm"
)

func ReconcileDerivedGroupDependencies(tx *gorm.DB, subscriptionID uint, defaultGroupID uint) error {
	if subscriptionID == 0 || defaultGroupID == 0 {
		return nil
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ?", subscriptionID).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return err
	}
	dependencies := groupDependencyIDs(connections)
	if len(dependencies) == 0 {
		return SyncLegacyGroupIDs(tx, subscriptionID)
	}
	if err := tx.
		Where("group_id = ? AND connection_id IN ?", defaultGroupID, mapKeys(dependencies)).
		Delete(&model.RemoteOutboundGroupConnection{}).Error; err != nil {
		return err
	}
	return SyncLegacyGroupIDs(tx, subscriptionID)
}

func FilterVisibleConnections(subscriptions []model.RemoteOutboundSubscription) {
	for index := range subscriptions {
		connections := subscriptions[index].Connections
		if len(connections) == 0 {
			continue
		}
		dependencies := groupDependencyIDs(connections)
		visible := connections[:0]
		for _, connection := range connections {
			if connection.Missing {
				continue
			}
			if _, ok := dependencies[connection.Id]; ok {
				continue
			}
			visible = append(visible, connection)
		}
		subscriptions[index].Connections = visible
	}
}

func syncGroupDependencyOutbounds(tx *gorm.DB, group model.RemoteOutboundConnection, requireUsable bool, visited map[uint]struct{}) error {
	members, err := groupDependencyConnections(tx, group)
	if err != nil {
		return err
	}
	for index := range members {
		if err := syncConnectionToOutbound(tx, &members[index], requireUsable, visited); err != nil {
			return fmt.Errorf("sync group dependency %q: %w", members[index].Name, err)
		}
		if err := tx.Save(&members[index]).Error; err != nil {
			return err
		}
	}
	return nil
}

func ExpandConnectionsWithGroupDependencies(tx *gorm.DB, connections []model.RemoteOutboundConnection) ([]model.RemoteOutboundConnection, error) {
	result := make([]model.RemoteOutboundConnection, 0, len(connections))
	seen := map[uint]struct{}{}
	for _, connection := range connections {
		if _, ok := seen[connection.Id]; !ok {
			seen[connection.Id] = struct{}{}
			result = append(result, connection)
		}
		if !remoteConnectionIsGroup(connection) {
			continue
		}
		members, err := groupDependencyConnections(tx, connection)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if _, ok := seen[member.Id]; ok {
				continue
			}
			seen[member.Id] = struct{}{}
			result = append(result, member)
		}
	}
	return result, nil
}

func unsyncGroupDependenciesIfUnused(tx *gorm.DB, group model.RemoteOutboundConnection) (int, error) {
	members, err := groupDependencyConnections(tx, group)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, member := range members {
		if !member.Synced {
			continue
		}
		used, err := dependencyUsedBySyncedGroup(tx, member)
		if err != nil || used {
			return removed, err
		}
		changed, err := UnsyncConnectionIfNoOutboundGroup(tx, member.Id)
		if err != nil {
			return removed, err
		}
		if changed {
			removed++
		}
	}
	return removed, nil
}

func dependencyUsedBySyncedGroup(tx *gorm.DB, member model.RemoteOutboundConnection) (bool, error) {
	if member.SubscriptionId == 0 {
		return false, nil
	}
	if used, err := ConnectionUsesOutboundEnabledGroup(tx, member.Id); err != nil || used {
		return used, err
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ? AND synced = ? AND id <> ?", member.SubscriptionId, true, member.Id).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return false, err
	}
	for _, connection := range connections {
		if !remoteConnectionIsGroup(connection) {
			continue
		}
		for _, ref := range groupMemberRefs(connection) {
			if connectionMatchesMemberRef(member, ref) {
				return true, nil
			}
		}
	}
	return false, nil
}

func groupDependencyConnections(tx *gorm.DB, group model.RemoteOutboundConnection) ([]model.RemoteOutboundConnection, error) {
	refs := groupMemberRefs(group)
	if len(refs) == 0 || group.SubscriptionId == 0 {
		return nil, nil
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ?", group.SubscriptionId).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return nil, err
	}
	result := make([]model.RemoteOutboundConnection, 0, len(refs))
	seen := map[uint]struct{}{}
	for _, ref := range refs {
		for _, connection := range connections {
			if connection.Id == group.Id {
				continue
			}
			if _, ok := seen[connection.Id]; ok {
				continue
			}
			if !connectionMatchesMemberRef(connection, ref) {
				continue
			}
			seen[connection.Id] = struct{}{}
			result = append(result, connection)
			break
		}
	}
	return result, nil
}

func groupDependencyIDs(connections []model.RemoteOutboundConnection) map[uint]struct{} {
	return technicalDependencyIDs(connections)
}

func technicalDependencyIDs(connections []model.RemoteOutboundConnection) map[uint]struct{} {
	result := map[uint]struct{}{}
	for _, connection := range connections {
		if connectionIsTechnicalDependency(connection) {
			result[connection.Id] = struct{}{}
		}
	}
	return result
}

func connectionIsTechnicalDependency(connection model.RemoteOutboundConnection) bool {
	if len(connection.Canonical) == 0 {
		return false
	}
	var canonicalConnection subcanonical.Connection
	if err := json.Unmarshal(connection.Canonical, &canonicalConnection); err != nil {
		return false
	}
	if canonicalConnection.Role == subcanonical.RoleMember {
		return true
	}
	for _, observation := range canonicalConnection.Observations {
		if value, ok := observation.Outbound["xray_profile_member"].(bool); ok && value {
			return true
		}
	}
	return false
}

func groupMemberRefs(connection model.RemoteOutboundConnection) []string {
	if len(connection.Options) == 0 {
		return nil
	}
	raw := map[string]any{}
	if err := json.Unmarshal(connection.Options, &raw); err != nil {
		return nil
	}
	return stringMemberList(raw["outbounds"])
}

func stringMemberList(value any) []string {
	switch typed := value.(type) {
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" && trimmed != "<nil>" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		return nil
	}
}

func connectionMatchesMemberRef(connection model.RemoteOutboundConnection, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	normalizedRef := subcanonical.NormalizeLabel(ref)
	for _, candidate := range []string{connection.Name, connection.SourceKey, connection.OutboundTag} {
		if strings.TrimSpace(candidate) == ref || subcanonical.NormalizeLabel(candidate) == normalizedRef {
			return true
		}
	}
	return false
}

func mapKeys(values map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
