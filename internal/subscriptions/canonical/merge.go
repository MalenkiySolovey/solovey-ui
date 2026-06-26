package canonical

import (
	"encoding/json"
	"reflect"
	"strings"
)

func MergeSnapshots(snapshots ...Snapshot) Snapshot {
	result := Snapshot{Version: SnapshotVersion}
	for _, snapshot := range snapshots {
		result.Extras = mergeObservations(result.Extras, snapshot.Extras)
		for _, connection := range snapshot.Connections {
			result = mergeSnapshotConnection(result, connection)
		}
	}
	return result
}

func MergeConnections(left Connection, right Connection) Connection {
	left.Kind = mergeKind(left.Kind, right.Kind)
	left.Role = mergeRole(left.Role, right.Role)
	if left.DisplayName == "" {
		left.DisplayName = right.DisplayName
	}
	if left.Protocol == "" {
		left.Protocol = right.Protocol
	}
	if left.Endpoint.Server == "" {
		left.Endpoint.Server = right.Endpoint.Server
	}
	if left.Endpoint.Port == "" {
		left.Endpoint.Port = right.Endpoint.Port
	}
	if !left.TLS.Enabled {
		left.TLS.Enabled = right.TLS.Enabled
	}
	if left.TLS.ServerName == "" {
		left.TLS.ServerName = right.TLS.ServerName
	}
	if !left.TLS.Reality {
		left.TLS.Reality = right.TLS.Reality
	}
	if left.Transport.Type == "" {
		left.Transport.Type = right.Transport.Type
	}
	if left.Transport.Path == "" {
		left.Transport.Path = right.Transport.Path
	}
	if left.Transport.Host == "" {
		left.Transport.Host = right.Transport.Host
	}
	left.GroupMembers = mergeStrings(left.GroupMembers, right.GroupMembers)
	left.BestOutbound = mergeMaps(left.BestOutbound, right.BestOutbound)
	left.Formats = mergeStrings(left.Formats, right.Formats)
	left.Adaptations = mergeAdaptations(left.Adaptations, right.Adaptations)
	left.Observations = mergeObservations(left.Observations, right.Observations)
	return left
}

func mergeKind(left string, right string) string {
	if left == KindGroup || right == KindGroup {
		return KindGroup
	}
	if left != "" {
		return left
	}
	if right != "" {
		return right
	}
	return KindSingle
}

func mergeRole(left string, right string) string {
	if left == RoleMember || right == RoleMember {
		return RoleMember
	}
	if left != "" {
		return left
	}
	if right != "" {
		return right
	}
	return RoleTopLevel
}

func mergeSnapshotConnection(snapshot Snapshot, connection Connection) Snapshot {
	if snapshot.Version == 0 {
		snapshot.Version = SnapshotVersion
	}
	for index := range snapshot.Connections {
		if sameNamedGroupRepresentation(snapshot.Connections[index], connection) {
			snapshot.Connections[index] = mergeGroupRepresentation(snapshot.Connections[index], connection)
			snapshot.Formats = mergeStrings(snapshot.Formats, connection.Formats)
			return snapshot
		}
		if !sameConnectionIdentity(snapshot.Connections[index], connection) {
			continue
		}
		snapshot.Connections[index] = MergeConnections(snapshot.Connections[index], connection)
		snapshot.Formats = mergeStrings(snapshot.Formats, connection.Formats)
		return snapshot
	}
	if clashProxyGroupComposesExistingTopLevel(snapshot, connection) {
		snapshot.Extras = mergeObservations(snapshot.Extras, connection.Observations)
		snapshot.Formats = mergeStrings(snapshot.Formats, connection.Formats)
		return snapshot
	}
	snapshot.Connections = append(snapshot.Connections, connection)
	snapshot.Formats = mergeStrings(snapshot.Formats, connection.Formats)
	return snapshot
}

func clashProxyGroupComposesExistingTopLevel(snapshot Snapshot, connection Connection) bool {
	if connection.Kind != KindGroup || connection.Role == RoleMember || len(connection.GroupMembers) == 0 {
		return false
	}
	if !auxiliaryClashProxyGroup(connection) {
		return false
	}
	existing := topLevelNameSet(snapshot)
	if len(existing) == 0 {
		return false
	}
	for _, member := range connection.GroupMembers {
		if _, ok := existing[NormalizeLabel(member)]; !ok {
			return false
		}
	}
	return true
}

func auxiliaryClashProxyGroup(connection Connection) bool {
	for _, adaptation := range connection.Adaptations {
		if adaptation.SourceFormat == FormatClash &&
			adaptation.SourceFeature == "proxy-groups" &&
			adaptation.SourceType == "select" {
			return true
		}
	}
	return false
}

func topLevelNameSet(snapshot Snapshot) map[string]struct{} {
	result := make(map[string]struct{}, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		if connection.Role == RoleMember {
			continue
		}
		name := NormalizeLabel(connection.DisplayName)
		if name != "" {
			result[name] = struct{}{}
		}
	}
	return result
}

func sameNamedGroupRepresentation(left Connection, right Connection) bool {
	if formatsOverlap(left.Formats, right.Formats) {
		return false
	}
	leftGroup := len(left.GroupMembers) > 0
	rightGroup := len(right.GroupMembers) > 0
	if leftGroup == rightGroup {
		return false
	}
	leftName := normalizedConnectionName(left.DisplayName)
	rightName := normalizedConnectionName(right.DisplayName)
	if leftName == "" || leftName != rightName {
		return false
	}
	return connectionHasExplicitLabel(left) && connectionHasExplicitLabel(right)
}

func mergeGroupRepresentation(left Connection, right Connection) Connection {
	group := left
	alias := right
	if len(group.GroupMembers) == 0 {
		group = right
		alias = left
	}
	group.Kind = KindGroup
	group.Role = RoleTopLevel
	group.Formats = mergeStrings(group.Formats, alias.Formats)
	group.Adaptations = mergeAdaptations(group.Adaptations, alias.Adaptations)
	group.Observations = mergeObservations(group.Observations, alias.Observations)
	return group
}

func sameConnectionIdentity(left Connection, right Connection) bool {
	if formatsOverlap(left.Formats, right.Formats) {
		return false
	}
	leftName := normalizedConnectionName(left.DisplayName)
	rightName := normalizedConnectionName(right.DisplayName)
	if leftName == "" || leftName != rightName {
		return false
	}
	if len(left.GroupMembers) > 0 != (len(right.GroupMembers) > 0) {
		return false
	}
	return true
}

func formatsOverlap(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, format := range left {
		format = strings.TrimSpace(format)
		if format == "" {
			continue
		}
		seen[format] = struct{}{}
	}
	for _, format := range right {
		if _, ok := seen[strings.TrimSpace(format)]; ok {
			return true
		}
	}
	return false
}

func normalizedConnectionName(value string) string {
	return NormalizeLabel(value)
}

func connectionHasExplicitLabel(connection Connection) bool {
	for _, observation := range connection.Observations {
		if outboundHasExplicitLabel(observation.Outbound, connection.DisplayName) {
			return true
		}
	}
	return false
}

func outboundHasExplicitLabel(outbound map[string]any, displayName string) bool {
	if outbound == nil {
		return false
	}
	displayName = normalizedConnectionName(displayName)
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		value, ok := outbound[key].(string)
		if ok && normalizedConnectionName(value) == displayName {
			return true
		}
	}
	return false
}

func mergeMaps(left map[string]any, right map[string]any) map[string]any {
	if len(left) == 0 {
		return cloneMap(right)
	}
	result := cloneMap(left)
	for key, rightValue := range right {
		leftValue, exists := result[key]
		if !exists || isEmpty(leftValue) {
			result[key] = cloneValue(rightValue)
			continue
		}
		leftMap, leftOK := leftValue.(map[string]any)
		rightMap, rightOK := rightValue.(map[string]any)
		if leftOK && rightOK {
			result[key] = mergeMaps(leftMap, rightMap)
		}
	}
	return result
}

func mergeStrings(left []string, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	result := make([]string, 0, len(left)+len(right))
	for _, value := range append(left, right...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func mergeObservations(left []Observation, right []Observation) []Observation {
	seen := make(map[string]bool, len(left)+len(right))
	result := make([]Observation, 0, len(left)+len(right))
	for _, observation := range append(left, right...) {
		key := observationKey(observation)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Observation{
			Format:   observation.Format,
			Name:     observation.Name,
			Outbound: cloneMap(observation.Outbound),
		})
	}
	return result
}

func mergeAdaptations(left []Adaptation, right []Adaptation) []Adaptation {
	seen := make(map[string]bool, len(left)+len(right))
	result := make([]Adaptation, 0, len(left)+len(right))
	for _, adaptation := range append(left, right...) {
		key := adaptationKey(adaptation)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, adaptation)
	}
	return result
}

func adaptationKey(adaptation Adaptation) string {
	if adaptation == (Adaptation{}) {
		return ""
	}
	data, _ := json.Marshal(adaptation)
	return string(data)
}

func observationKey(observation Observation) string {
	data, _ := json.Marshal(observation.Outbound)
	return observation.Format + ":" + string(data)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneValue(value)
	}
	return output
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneValue(item)
		}
		return result
	default:
		return typed
	}
}

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return reflect.ValueOf(value).IsZero()
	}
}
