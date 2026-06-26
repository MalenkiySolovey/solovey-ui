package remotesubservice

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func collectedProfile(subscription model.RemoteOutboundSubscription) []CollectedProfileBlock {
	snapshot, ok := parseCanonicalSnapshot(subscription.CanonicalSnapshot)
	if !ok || len(snapshot.Connections) == 0 {
		return nil
	}
	return profileFromSnapshot(snapshot)
}
func profileFromSnapshot(snapshot subcanonical.Snapshot) []CollectedProfileBlock {
	memberRefs := referencedGroupMembers(snapshot.Connections)
	blocks := make([]CollectedProfileBlock, 0, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		if connection.Role == subcanonical.RoleMember {
			continue
		}
		if len(connection.GroupMembers) == 0 {
			continue
		}
		blocks = append(blocks, profileBlockForConnection(connection, snapshot.Connections))
	}
	for _, connection := range snapshot.Connections {
		if connection.Role == subcanonical.RoleMember {
			continue
		}
		if len(connection.GroupMembers) > 0 {
			continue
		}
		if _, referenced := memberRefs[connectionRefKey(connection)]; referenced {
			continue
		}
		blocks = append(blocks, profileBlockForConnection(connection, snapshot.Connections))
	}
	return blocks
}
func profileBlockForConnection(connection subcanonical.Connection, all []subcanonical.Connection) CollectedProfileBlock {
	block := CollectedProfileBlock{
		Name:            displayConnectionName(connection),
		Type:            profileConnectionType(connection),
		Sources:         profileConnectionSources(connection),
		Characteristics: profileCharacteristics(connection),
	}
	block.Characteristics = append(block.Characteristics, xrayProfileBalancerCharacteristics(connection)...)
	if len(connection.GroupMembers) == 0 {
		block.Connections = append(block.Connections, xrayProfileMemberBlocks(connection)...)
		return block
	}
	for _, memberRef := range connection.GroupMembers {
		member, ok := findProfileMemberConnection(all, memberRef)
		if !ok {
			block.Connections = append(block.Connections, CollectedProfileBlock{
				Name: memberRef,
				Type: "missing saved connection",
				Characteristics: []CollectedProfileCharacteristic{{
					Key:   "outbounds",
					Label: "Group reference",
					Values: []CollectedProfileValue{{
						Value:   memberRef,
						Sources: profileConnectionSources(connection),
					}},
				}},
			})
			continue
		}
		block.Connections = append(block.Connections, profileBlockForConnection(member, all))
	}
	return block
}
func collectedSummary(subscription model.RemoteOutboundSubscription, profile []CollectedProfileBlock) string {
	if len(profile) == 0 {
		return "Internal subscription data has not been collected yet. Refresh the subscription to build a profile."
	}
	var buffer bytes.Buffer
	if strings.TrimSpace(subscription.Name) != "" {
		fmt.Fprintf(&buffer, "Subscription: %s\n", subscription.Name)
	}
	if strings.TrimSpace(subscription.Url) != "" {
		fmt.Fprintf(&buffer, "Source: %s\n", subscription.Url)
	}
	if subscription.LastUpdated > 0 {
		fmt.Fprintf(&buffer, "Last successful update: %d\n", subscription.LastUpdated)
	}
	if strings.TrimSpace(subscription.LastError) != "" {
		fmt.Fprintf(&buffer, "Last error: %s\n", subscription.LastError)
	}
	for index, block := range profile {
		if index > 0 || buffer.Len() > 0 {
			buffer.WriteString("\n")
		}
		writeProfileBlock(&buffer, block, "")
	}
	return strings.TrimRight(buffer.String(), "\n")
}
func writeProfileBlock(buffer *bytes.Buffer, block CollectedProfileBlock, indent string) {
	fmt.Fprintf(buffer, "%sName: %s\n", indent, block.Name)
	fmt.Fprintf(buffer, "%sType: %s%s\n", indent, block.Type, sourceSuffix(block.Sources))
	writeCharacteristics(buffer, block.Characteristics, indent)
	for index, connection := range block.Connections {
		buffer.WriteString("\n")
		fmt.Fprintf(buffer, "%sConnection %d:\n", indent, index+1)
		writeProfileBlock(buffer, connection, indent+"   ")
	}
}
func writeCharacteristics(buffer *bytes.Buffer, characteristics []CollectedProfileCharacteristic, indent string) {
	if len(characteristics) == 0 {
		return
	}
	fmt.Fprintf(buffer, "%sCharacteristics:\n", indent)
	for _, characteristic := range characteristics {
		for valueIndex, value := range characteristic.Values {
			label := characteristic.Label
			if valueIndex > 0 {
				label = label + " " + fmt.Sprintf("(%d)", valueIndex+1)
			}
			fmt.Fprintf(buffer, "%s   %s: %s%s\n", indent, label, value.Value, sourceSuffix(value.Sources))
		}
	}
}
func profileCharacteristics(connection subcanonical.Connection) []CollectedProfileCharacteristic {
	valuesByKey := map[string]map[string]map[string]struct{}{}
	labelsByKey := map[string]string{}
	for _, observation := range connection.Observations {
		source := sourceLabel(observation.Format)
		for _, field := range flattenObservationFields(observation.Outbound, "") {
			if valuesByKey[field.Key] == nil {
				valuesByKey[field.Key] = map[string]map[string]struct{}{}
			}
			if valuesByKey[field.Key][field.Value] == nil {
				valuesByKey[field.Key][field.Value] = map[string]struct{}{}
			}
			valuesByKey[field.Key][field.Value][source] = struct{}{}
			labelsByKey[field.Key] = field.Label
		}
	}
	keys := make([]string, 0, len(valuesByKey))
	for key := range valuesByKey {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return characteristicOrder(keys[i]) < characteristicOrder(keys[j]) ||
			(characteristicOrder(keys[i]) == characteristicOrder(keys[j]) && labelsByKey[keys[i]] < labelsByKey[keys[j]])
	})
	result := make([]CollectedProfileCharacteristic, 0, len(keys))
	for _, key := range keys {
		values := make([]CollectedProfileValue, 0, len(valuesByKey[key]))
		for value, sources := range valuesByKey[key] {
			values = append(values, CollectedProfileValue{
				Value:   value,
				Sources: sortedSourceSet(sources),
			})
		}
		sort.SliceStable(values, func(i, j int) bool {
			return values[i].Value < values[j].Value
		})
		result = append(result, CollectedProfileCharacteristic{
			Key:    key,
			Label:  labelsByKey[key],
			Values: values,
		})
	}
	return result
}
func xrayProfileMemberBlocks(connection subcanonical.Connection) []CollectedProfileBlock {
	blocks := make([]CollectedProfileBlock, 0)
	for _, observation := range connection.Observations {
		if observation.Format != subcanonical.FormatXray {
			continue
		}
		for index, member := range profileMapList(observation.Outbound["xray_profile_outbounds"]) {
			memberConnection := subcanonical.ObserveOutbound(observation.Format, member, index)
			blocks = append(blocks, profileBlockForConnection(memberConnection, nil))
		}
	}
	return blocks
}
func xrayProfileBalancerCharacteristics(connection subcanonical.Connection) []CollectedProfileCharacteristic {
	valuesByKey := map[string]map[string]map[string]struct{}{}
	labelsByKey := map[string]string{}
	for _, observation := range connection.Observations {
		if observation.Format != subcanonical.FormatXray {
			continue
		}
		source := sourceLabel(observation.Format)
		for _, balancer := range profileMapList(observation.Outbound["xray_profile_balancers"]) {
			addProfileCharacteristicValue(valuesByKey, labelsByKey, "xray_balancer", "Xray balancer", profileValueString(balancer["tag"]), source)
			addProfileCharacteristicValue(valuesByKey, labelsByKey, "xray_balancer.selector", "Selector", profileValueString(balancer["selector"]), source)
			addProfileCharacteristicValue(valuesByKey, labelsByKey, "xray_balancer.members", "Members", profileValueString(balancer["members"]), source)
			addProfileCharacteristicValue(valuesByKey, labelsByKey, "xray_balancer.fallback", "Fallback", profileValueString(balancer["fallback_tag"]), source)
			addProfileCharacteristicValue(valuesByKey, labelsByKey, "xray_balancer.strategy", "Strategy", profileValueString(balancer["strategy"]), source)
		}
	}
	return profileCharacteristicsFromValues(valuesByKey, labelsByKey)
}
func profileConnectionType(connection subcanonical.Connection) string {
	if profileType, ok := xrayProfileType(connection); ok {
		if profileType == "balancer" {
			return "x-ray JSON balancer group"
		}
		return "x-ray JSON profile"
	}
	if len(connection.GroupMembers) > 0 {
		for _, adaptation := range connection.Adaptations {
			if adaptation.SourceFormat == subcanonical.FormatXray && adaptation.SourceFeature == "routing.balancer" {
				return "balancer group"
			}
			if adaptation.SourceFormat == subcanonical.FormatClash && adaptation.SourceFeature == "proxy-groups" {
				sourceType := strings.TrimSpace(adaptation.SourceType)
				if sourceType != "" {
					return "mihomo " + sourceType + " group"
				}
				return "mihomo group"
			}
		}
		if connection.Protocol != "" {
			return connection.Protocol + " group"
		}
		return "group"
	}
	if connection.Protocol != "" {
		return "single " + connection.Protocol
	}
	return "single connection"
}
func xrayProfileType(connection subcanonical.Connection) (string, bool) {
	for _, observation := range connection.Observations {
		if observation.Format != subcanonical.FormatXray {
			continue
		}
		if !profileBoolValue(observation.Outbound["xray_profile"]) {
			continue
		}
		profileType := strings.TrimSpace(profileValueString(observation.Outbound["xray_profile_type"]))
		if profileType == "" {
			profileType = "custom"
		}
		return profileType, true
	}
	for _, adaptation := range connection.Adaptations {
		if adaptation.SourceFormat == subcanonical.FormatXray && adaptation.SourceFeature == "custom.config" {
			profileType := strings.TrimSpace(adaptation.SourceType)
			if profileType == "" {
				profileType = "custom"
			}
			return profileType, true
		}
	}
	return "", false
}
func displayConnectionName(connection subcanonical.Connection) string {
	if strings.TrimSpace(connection.DisplayName) != "" {
		return strings.TrimSpace(connection.DisplayName)
	}
	for _, observation := range connection.Observations {
		if strings.TrimSpace(observation.Name) != "" {
			return strings.TrimSpace(observation.Name)
		}
	}
	return "connection"
}
func referencedGroupMembers(connections []subcanonical.Connection) map[string]struct{} {
	result := map[string]struct{}{}
	for _, connection := range connections {
		for _, member := range connection.GroupMembers {
			memberConnection, ok := findProfileMemberConnection(connections, member)
			if !ok {
				continue
			}
			result[connectionRefKey(memberConnection)] = struct{}{}
		}
	}
	return result
}
func findProfileMemberConnection(connections []subcanonical.Connection, ref string) (subcanonical.Connection, bool) {
	ref = strings.TrimSpace(ref)
	for _, connection := range connections {
		if connectionMatchesRef(connection, ref) {
			return connection, true
		}
	}
	return subcanonical.Connection{}, false
}
func connectionMatchesRef(connection subcanonical.Connection, ref string) bool {
	if ref == "" {
		return false
	}
	normalizedRef := subcanonical.NormalizeLabel(ref)
	candidates := []string{connection.DisplayName}
	for _, observation := range connection.Observations {
		candidates = append(candidates, observation.Name)
		for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
			if value, ok := observation.Outbound[key].(string); ok {
				candidates = append(candidates, value)
			}
		}
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == strings.TrimSpace(ref) || subcanonical.NormalizeLabel(candidate) == normalizedRef {
			return true
		}
	}
	return false
}
func connectionRefKey(connection subcanonical.Connection) string {
	return subcanonical.NormalizeLabel(displayConnectionName(connection))
}
