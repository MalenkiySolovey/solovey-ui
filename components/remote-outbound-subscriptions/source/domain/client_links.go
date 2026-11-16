//go:build !minimal

package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-host/subscriptions/conversion"
)

type clientLinkSelection struct {
	Kind string
	ID   uint
}

func OutboundsForClientLinks(db Store, rawLinks json.RawMessage) ([]map[string]interface{}, []string, error) {
	return OutboundsForClientLinksWithOptions(db, rawLinks, ClientConversionOptions{
		Target: subconversion.TargetSingBox,
		Policy: subconversion.DefaultPolicy(),
	})
}
func OutboundsForClientLinksWithOptions(db Store, rawLinks json.RawMessage, options ClientConversionOptions) ([]map[string]interface{}, []string, error) {
	links := []ClientLink{}
	if len(rawLinks) == 0 {
		return nil, nil, nil
	}
	if err := json.Unmarshal(rawLinks, &links); err != nil {
		return nil, nil, nil
	}

	selections := clientLinkSelections(links)
	if len(selections) == 0 {
		return nil, nil, nil
	}

	connections := make([]RemoteOutboundConnection, 0)
	for _, selection := range selections {
		selected, err := clientLinkSelectionConnections(db, selection)
		if err != nil {
			return nil, nil, err
		}
		connections = append(connections, selected...)
	}
	if len(connections) == 0 {
		return nil, nil, nil
	}

	var err error
	connections, err = ExpandConnectionsWithGroupDependencies(db, connections)
	if err != nil {
		return nil, nil, err
	}
	tagMap := ConnectionTagMap(connections)

	outbounds := make([]map[string]interface{}, 0, len(connections))
	tags := make([]string, 0, len(connections))
	seenConnections := map[uint]struct{}{}
	for _, connection := range connections {
		if _, ok := seenConnections[connection.Id]; ok {
			continue
		}
		seenConnections[connection.Id] = struct{}{}
		outbound, err := ConnectionOutboundMapForClient(connection, tagMap, options)
		if err != nil {
			return nil, nil, err
		}
		tag, _ := outbound["tag"].(string)
		if strings.TrimSpace(tag) == "" {
			continue
		}
		outbounds = append(outbounds, outbound)
		tags = append(tags, tag)
	}
	outbounds, tags, err = appendClientTargetExtras(db, outbounds, tags, connections, tagMap, options.Target)
	if err != nil {
		return nil, nil, err
	}
	return outbounds, tags, nil
}
func clientLinkSelections(links []ClientLink) []clientLinkSelection {
	selections := make([]clientLinkSelection, 0, len(links))
	seen := map[string]struct{}{}
	for _, link := range links {
		kind := strings.TrimSpace(link.Type)
		id := uint(0)
		switch kind {
		case "remoteGroup":
			id = link.GroupId
			if id == 0 {
				id = link.RemoteGroupId
			}
		case "remoteSubscription":
			id = link.SubscriptionId
			if id == 0 {
				id = link.RemoteSubscriptionId
			}
		default:
			continue
		}
		if id == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d", kind, id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		selections = append(selections, clientLinkSelection{Kind: kind, ID: id})
	}
	return selections
}
func clientLinkSelectionConnections(db Store, selection clientLinkSelection) ([]RemoteOutboundConnection, error) {
	switch selection.Kind {
	case "remoteGroup":
		return clientRemoteGroupConnections(db, selection.ID)
	case "remoteSubscription":
		return clientRemoteSubscriptionConnections(db, selection.ID)
	default:
		return nil, nil
	}
}
func clientRemoteGroupConnections(db Store, groupID uint) ([]RemoteOutboundConnection, error) {
	var connections []RemoteOutboundConnection
	err := db.ListClientGroupConnections(groupID, &connections)
	return connections, err
}
func clientRemoteSubscriptionConnections(db Store, subscriptionID uint) ([]RemoteOutboundConnection, error) {
	var connections []RemoteOutboundConnection
	err := db.ListClientSubscriptionConnections(subscriptionID, &connections)
	if err != nil {
		return nil, err
	}
	subscriptions := []RemoteOutboundSubscription{{Connections: connections}}
	FilterVisibleConnections(subscriptions)
	return subscriptions[0].Connections, nil
}
func appendClientTargetExtras(db Store, outbounds []map[string]interface{}, tags []string, connections []RemoteOutboundConnection, tagMap map[string]string, target string) ([]map[string]interface{}, []string, error) {
	if strings.TrimSpace(target) != subconversion.TargetMihomo {
		return outbounds, tags, nil
	}
	subscriptionIDs := make([]uint, 0)
	seenSubscriptions := map[uint]struct{}{}
	for _, connection := range connections {
		if connection.SubscriptionId == 0 {
			continue
		}
		if _, ok := seenSubscriptions[connection.SubscriptionId]; ok {
			continue
		}
		seenSubscriptions[connection.SubscriptionId] = struct{}{}
		subscriptionIDs = append(subscriptionIDs, connection.SubscriptionId)
	}
	if len(subscriptionIDs) == 0 {
		return outbounds, tags, nil
	}
	var subscriptions []RemoteOutboundSubscription
	if err := db.ListSelectedSubscriptions(subscriptionIDs, &subscriptions); err != nil {
		return nil, nil, err
	}
	for _, subscription := range subscriptions {
		var snapshot subcanonical.Snapshot
		if len(subscription.CanonicalSnapshot) == 0 || json.Unmarshal(subscription.CanonicalSnapshot, &snapshot) != nil {
			continue
		}
		for _, extra := range snapshot.Extras {
			extraOutbound := mihomoExtraGroupOutbound(extra, tagMap)
			if extraOutbound == nil {
				continue
			}
			outbounds = append(outbounds, extraOutbound)
			if tag := strings.TrimSpace(fmt.Sprint(extraOutbound["tag"])); tag != "" && tag != "<nil>" {
				tags = append(tags, tag)
			}
		}
	}
	return outbounds, tags, nil
}
func mihomoExtraGroupOutbound(extra subcanonical.Observation, tagMap map[string]string) map[string]interface{} {
	if extra.Format != subcanonical.FormatClash || len(extra.Outbound) == 0 {
		return nil
	}
	outbound := cloneOutboundMap(extra.Outbound)
	RewriteOutboundTagReferences(outbound, tagMap)
	if len(StringList(outbound["outbounds"])) == 0 {
		return nil
	}
	return outbound
}
