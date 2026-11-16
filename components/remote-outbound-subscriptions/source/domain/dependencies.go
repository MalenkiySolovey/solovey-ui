//go:build !minimal

package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
)

func ReconcileDerivedGroupDependencies(tx Store, subscriptionID uint, defaultGroupID uint) error {
	if subscriptionID == 0 || defaultGroupID == 0 {
		return nil
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListOrderedSubscriptionConnections(subscriptionID, &connections); err != nil {
		return err
	}
	dependencies := groupDependencyIDs(connections)
	if len(dependencies) == 0 {
		return SyncLegacyGroupIDs(tx, subscriptionID)
	}
	if err := tx.DeleteSelectedMemberships(defaultGroupID, mapKeys(dependencies)); err != nil {
		return err
	}
	return SyncLegacyGroupIDs(tx, subscriptionID)
}

func FilterVisibleConnections(subscriptions []RemoteOutboundSubscription) {
	for index := range subscriptions {
		connections := subscriptions[index].Connections
		if len(connections) == 0 {
			continue
		}
		dependencies := groupDependencyIDs(connections)
		visible := connections[:0]
		for _, connection := range connections {
			if _, ok := dependencies[connection.Id]; ok {
				continue
			}
			visible = append(visible, connection)
		}
		subscriptions[index].Connections = visible
	}
}

func syncGroupDependencyOutbounds(tx Store, group RemoteOutboundConnection, requireUsable bool, visited map[uint]struct{}) error {
	members, err := GroupDependencyConnections(tx, group)
	if err != nil {
		return err
	}
	for index := range members {
		if err := syncConnectionToOutbound(tx, &members[index], requireUsable, visited); err != nil {
			return fmt.Errorf("sync group dependency %q: %w", members[index].Name, err)
		}
		if err := tx.SaveConnection(&members[index]); err != nil {
			return err
		}
	}
	return nil
}

func ExpandConnectionsWithGroupDependencies(tx Store, connections []RemoteOutboundConnection) ([]RemoteOutboundConnection, error) {
	result := make([]RemoteOutboundConnection, 0, len(connections))
	seen := map[uint]struct{}{}
	for _, connection := range connections {
		if _, ok := seen[connection.Id]; !ok {
			seen[connection.Id] = struct{}{}
			result = append(result, connection)
		}
	}
	// Process appended groups too: native groups may borrow other groups. The
	// identity set bounds cycles; the canonical owner validates their semantics.
	for index := 0; index < len(result); index++ {
		connection := result[index]
		if !ConnectionIsGroup(connection) {
			continue
		}
		members, err := GroupDependencyConnections(tx, connection)
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

func unsyncGroupDependenciesIfUnused(tx Store, group RemoteOutboundConnection) (int, error) {
	members, err := GroupDependencyConnections(tx, group)
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

func dependencyUsedBySyncedGroup(tx Store, member RemoteOutboundConnection) (bool, error) {
	if member.SubscriptionId == 0 {
		return false, nil
	}
	if used, err := ConnectionUsesOutboundEnabledGroup(tx, member.Id); err != nil || used {
		return used, err
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListOtherSyncedConnections(member.SubscriptionId, member.Id, &connections); err != nil {
		return false, err
	}
	for _, connection := range connections {
		if !ConnectionIsGroup(connection) {
			continue
		}
		for _, ref := range GroupMemberRefs(connection) {
			if ConnectionMatchesMemberRef(member, ref) {
				return true, nil
			}
		}
	}
	return false, nil
}

func GroupDependencyConnections(tx Store, group RemoteOutboundConnection) ([]RemoteOutboundConnection, error) {
	refs := GroupMemberRefs(group)
	if len(refs) == 0 || group.SubscriptionId == 0 {
		return nil, nil
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListOrderedSubscriptionConnections(group.SubscriptionId, &connections); err != nil {
		return nil, err
	}
	result := make([]RemoteOutboundConnection, 0, len(refs))
	seen := map[uint]struct{}{}
	for _, ref := range refs {
		for _, connection := range connections {
			if connection.Id == group.Id {
				continue
			}
			if _, ok := seen[connection.Id]; ok {
				continue
			}
			if !ConnectionMatchesMemberRef(connection, ref) {
				continue
			}
			seen[connection.Id] = struct{}{}
			result = append(result, connection)
			break
		}
	}
	return result, nil
}

func groupDependencyIDs(connections []RemoteOutboundConnection) map[uint]struct{} {
	return technicalDependencyIDs(connections)
}

func technicalDependencyIDs(connections []RemoteOutboundConnection) map[uint]struct{} {
	result := map[uint]struct{}{}
	for _, connection := range connections {
		if connectionIsTechnicalDependency(connection) {
			result[connection.Id] = struct{}{}
		}
	}
	return result
}

func connectionIsTechnicalDependency(connection RemoteOutboundConnection) bool {
	if len(connection.Canonical) == 0 {
		return false
	}
	var CanonicalConnection subcanonical.Connection
	if err := json.Unmarshal(connection.Canonical, &CanonicalConnection); err != nil {
		return false
	}
	if CanonicalConnection.Role == subcanonical.RoleMember {
		return true
	}
	for _, observation := range CanonicalConnection.Observations {
		if value, ok := observation.Outbound["xray_profile_member"].(bool); ok && value {
			return true
		}
	}
	return false
}

func GroupMemberRefs(connection RemoteOutboundConnection) []string {
	if len(connection.Options) == 0 {
		return nil
	}
	raw := map[string]any{}
	if err := json.Unmarshal(connection.Options, &raw); err != nil {
		return nil
	}
	raw["type"] = connection.Type
	return subcanonical.ObserveOutbound(subcanonical.FormatSingBox, raw, 0).GroupMembers
}

func ConnectionMatchesMemberRef(connection RemoteOutboundConnection, ref string) bool {
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
