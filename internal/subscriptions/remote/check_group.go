package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"gorm.io/gorm"
)

func groupCheckTags(connection model.RemoteOutboundConnection, connections []model.RemoteOutboundConnection, tagMap map[string]string) []string {
	if !remoteConnectionIsGroup(connection) {
		return []string{connection.OutboundTag}
	}
	available := groupCheckAvailableTags(connection, connections)
	result := make([]string, 0, len(connections))
	addRef := func(ref string) {
		tag := strings.TrimSpace(rewriteOutboundTag(ref, tagMap))
		if tag == "" || tag == connection.OutboundTag {
			return
		}
		if _, ok := available[tag]; !ok {
			return
		}
		result = append(result, tag)
	}

	members := groupMemberRefs(connection)
	if connection.Type != "urltest" {
		addRef(groupDefaultRef(connection))
		if len(result) > 0 {
			return uniqueCheckTags(result)
		}
		if len(members) > 0 {
			addRef(members[0])
			if len(result) > 0 {
				return uniqueCheckTags(result)
			}
		}
	}
	for _, ref := range members {
		addRef(ref)
	}
	if len(result) > 0 {
		return uniqueCheckTags(result)
	}
	for _, item := range connections {
		if item.Id == connection.Id && connection.Id != 0 {
			continue
		}
		if remoteConnectionIsGroup(item) {
			continue
		}
		if tag := strings.TrimSpace(item.OutboundTag); tag != "" && tag != connection.OutboundTag {
			result = append(result, tag)
		}
	}
	return uniqueCheckTags(result)
}
func groupCheckAvailableTags(connection model.RemoteOutboundConnection, connections []model.RemoteOutboundConnection) map[string]struct{} {
	result := make(map[string]struct{}, len(connections))
	for _, item := range connections {
		if item.Id == connection.Id && connection.Id != 0 {
			continue
		}
		if remoteConnectionIsGroup(item) {
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
func groupDefaultRef(connection model.RemoteOutboundConnection) string {
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
func groupCheckConnections(tx *gorm.DB, connection model.RemoteOutboundConnection) ([]model.RemoteOutboundConnection, error) {
	result := make([]model.RemoteOutboundConnection, 0)
	seen := map[uint]struct{}{}
	if err := appendGroupCheckConnection(tx, connection, seen, &result); err != nil {
		return nil, err
	}
	return result, nil
}
func appendGroupCheckConnection(tx *gorm.DB, connection model.RemoteOutboundConnection, seen map[uint]struct{}, result *[]model.RemoteOutboundConnection) error {
	if connection.Id != 0 {
		if _, ok := seen[connection.Id]; ok {
			return nil
		}
		seen[connection.Id] = struct{}{}
	}
	if remoteConnectionIsGroup(connection) {
		members, err := groupDependencyConnections(tx, connection)
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
func remoteConnectionIsGroup(connection model.RemoteOutboundConnection) bool {
	switch connection.Type {
	case "selector", "urltest", entityoutbounds.FailoverType:
		return true
	default:
		return false
	}
}
