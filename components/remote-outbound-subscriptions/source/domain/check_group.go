//go:build !minimal

package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
)

func GroupCheckTags(connection RemoteOutboundConnection, connections []RemoteOutboundConnection, tagMap map[string]string) []string {
	if !ConnectionIsGroup(connection) {
		return []string{connection.OutboundTag}
	}
	available := groupCheckAvailableTags(connection, connections)
	result := make([]string, 0, len(connections))
	addRef := func(ref string) {
		tag := strings.TrimSpace(RewriteOutboundTag(ref, tagMap))
		if tag == "" || tag == connection.OutboundTag {
			return
		}
		if _, ok := available[tag]; !ok {
			return
		}
		result = append(result, tag)
	}

	members := GroupMemberRefs(connection)
	if connection.Type != "urltest" {
		addRef(groupDefaultRef(connection))
		if len(result) > 0 {
			return UniqueCheckTags(result)
		}
		if len(members) > 0 {
			addRef(members[0])
			if len(result) > 0 {
				return UniqueCheckTags(result)
			}
		}
	}
	for _, ref := range members {
		addRef(ref)
	}
	if len(result) > 0 {
		return UniqueCheckTags(result)
	}
	for _, item := range connections {
		if item.Id == connection.Id && connection.Id != 0 {
			continue
		}
		if ConnectionIsGroup(item) {
			continue
		}
		if tag := strings.TrimSpace(item.OutboundTag); tag != "" && tag != connection.OutboundTag {
			result = append(result, tag)
		}
	}
	return UniqueCheckTags(result)
}
func groupCheckAvailableTags(connection RemoteOutboundConnection, connections []RemoteOutboundConnection) map[string]struct{} {
	result := make(map[string]struct{}, len(connections))
	for _, item := range connections {
		if item.Id == connection.Id && connection.Id != 0 {
			continue
		}
		if ConnectionIsGroup(item) {
			continue
		}
		tag := strings.TrimSpace(item.OutboundTag)
		if tag == "" || tag == connection.OutboundTag {
			continue
		}
		result[tag] = struct{}{}
	}
	return result
}
func groupDefaultRef(connection RemoteOutboundConnection) string {
	if len(connection.Options) == 0 {
		return ""
	}
	raw := map[string]any{}
	if err := json.Unmarshal(connection.Options, &raw); err != nil {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(raw["default"]))
	if value == "<nil>" {
		return ""
	}
	return value
}
func GroupCheckConnections(tx Store, connection RemoteOutboundConnection) ([]RemoteOutboundConnection, error) {
	result := make([]RemoteOutboundConnection, 0)
	seen := map[uint]struct{}{}
	if err := appendGroupCheckConnection(tx, connection, seen, &result); err != nil {
		return nil, err
	}
	return result, nil
}
func appendGroupCheckConnection(tx Store, connection RemoteOutboundConnection, seen map[uint]struct{}, result *[]RemoteOutboundConnection) error {
	if connection.Id != 0 {
		if _, ok := seen[connection.Id]; ok {
			return nil
		}
		seen[connection.Id] = struct{}{}
	}
	if ConnectionIsGroup(connection) {
		members, err := GroupDependencyConnections(tx, connection)
		if err != nil {
			return err
		}
		for _, member := range members {
			if err := appendGroupCheckConnection(tx, member, seen, result); err != nil {
				return err
			}
		}
	}
	*result = append(*result, connection)
	return nil
}
func ConnectionIsGroup(connection RemoteOutboundConnection) bool {
	if hostoutbounds.IsLimiter(connection.Type) {
		return true
	}
	switch connection.Type {
	case "selector", "urltest", "failover", "bond", "core-failover":
		return true
	default:
		return false
	}
}
