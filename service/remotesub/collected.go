package remotesubservice

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type CollectedSubscriptionData struct {
	SubscriptionId uint                      `json:"subscriptionId"`
	Name           string                    `json:"name"`
	URL            string                    `json:"url"`
	LastUpdated    int64                     `json:"lastUpdated"`
	LastError      string                    `json:"lastError,omitempty"`
	Summary        string                    `json:"summary,omitempty"`
	Profile        []CollectedProfileBlock   `json:"profile,omitempty"`
	Snapshot       any                       `json:"snapshot,omitempty"`
	Collection     any                       `json:"collection,omitempty"`
	Connections    []CollectedConnectionData `json:"connections"`
}

type CollectedProfileBlock struct {
	Name            string                           `json:"name"`
	Type            string                           `json:"type"`
	Sources         []string                         `json:"sources,omitempty"`
	Characteristics []CollectedProfileCharacteristic `json:"characteristics,omitempty"`
	Connections     []CollectedProfileBlock          `json:"connections,omitempty"`
}

type CollectedProfileCharacteristic struct {
	Key    string                  `json:"key"`
	Label  string                  `json:"label"`
	Values []CollectedProfileValue `json:"values"`
}

type CollectedProfileValue struct {
	Value   string   `json:"value"`
	Sources []string `json:"sources"`
}

type CollectedConnectionData struct {
	Id            uint   `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	SourceKey     string `json:"sourceKey"`
	OutboundTag   string `json:"outboundTag"`
	Enabled       bool   `json:"enabled"`
	Missing       bool   `json:"missing"`
	MissingReason string `json:"missingReason,omitempty"`
	MissingSince  int64  `json:"missingSince,omitempty"`
	LastSeen      int64  `json:"lastSeen"`
	Options       any    `json:"options,omitempty"`
	Canonical     any    `json:"canonical,omitempty"`
}

type CanonicalSnapshotView struct {
	Version     int                        `json:"version"`
	Formats     []string                   `json:"formats,omitempty"`
	Connections []CanonicalConnectionView  `json:"connections,omitempty"`
	Extras      []subcanonical.Observation `json:"extras,omitempty"`
}

type CanonicalConnectionView struct {
	Kind         string                    `json:"kind,omitempty"`
	Role         string                    `json:"role,omitempty"`
	DisplayName  string                    `json:"displayName,omitempty"`
	Protocol     string                    `json:"protocol,omitempty"`
	Endpoint     subcanonical.Endpoint     `json:"endpoint,omitempty"`
	TLS          subcanonical.TLS          `json:"tls,omitempty"`
	Transport    subcanonical.Transport    `json:"transport,omitempty"`
	GroupMembers []string                  `json:"groupMembers,omitempty"`
	Formats      []string                  `json:"formats,omitempty"`
	Adaptations  []subcanonical.Adaptation `json:"adaptations,omitempty"`
}

func (s *Service) GetCollectedData(id uint) (*CollectedSubscriptionData, error) {
	if id == 0 {
		return nil, common.NewError("subscription id is required")
	}
	var subscription model.RemoteOutboundSubscription
	if err := dbsqlite.DB().
		Preload("Connections", func(db *gorm.DB) *gorm.DB {
			return db.Order(entityorder.Clause)
		}).
		First(&subscription, id).Error; err != nil {
		return nil, err
	}
	profile := collectedProfile(subscription)
	result := &CollectedSubscriptionData{
		SubscriptionId: subscription.Id,
		Name:           subscription.Name,
		URL:            subscription.Url,
		LastUpdated:    subscription.LastUpdated,
		LastError:      subscription.LastError,
		Summary:        collectedSummary(subscription, profile),
		Profile:        profile,
		Snapshot:       canonicalSnapshotView(subscription.CanonicalSnapshot),
		Collection:     jsonValue(subscription.CollectionSnapshot),
		Connections:    make([]CollectedConnectionData, 0, len(subscription.Connections)),
	}
	for _, connection := range subscription.Connections {
		result.Connections = append(result.Connections, CollectedConnectionData{
			Id:            connection.Id,
			Name:          connection.Name,
			Type:          connection.Type,
			SourceKey:     connection.SourceKey,
			OutboundTag:   connection.OutboundTag,
			Enabled:       connection.Enabled,
			Missing:       connection.Missing,
			MissingReason: connection.MissingReason,
			MissingSince:  connection.MissingSince,
			LastSeen:      connection.LastSeen,
			Options:       jsonValue(connection.Options),
			Canonical:     canonicalConnectionView(connection.Canonical),
		})
	}
	return result, nil
}

func parseCanonicalSnapshot(raw json.RawMessage) (subcanonical.Snapshot, bool) {
	if len(raw) == 0 {
		return subcanonical.Snapshot{}, false
	}
	var snapshot subcanonical.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return subcanonical.Snapshot{}, false
	}
	return snapshot, true
}

func canonicalSnapshotView(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var snapshot subcanonical.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return jsonValue(raw)
	}
	view := CanonicalSnapshotView{
		Version:     snapshot.Version,
		Formats:     append([]string(nil), snapshot.Formats...),
		Connections: make([]CanonicalConnectionView, 0, len(snapshot.Connections)),
		Extras:      append([]subcanonical.Observation(nil), snapshot.Extras...),
	}
	for _, connection := range snapshot.Connections {
		view.Connections = append(view.Connections, compactCanonicalConnection(connection))
	}
	return view
}

func canonicalConnectionView(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var connection subcanonical.Connection
	if err := json.Unmarshal(raw, &connection); err != nil {
		return jsonValue(raw)
	}
	return compactCanonicalConnection(connection)
}

func compactCanonicalConnection(connection subcanonical.Connection) CanonicalConnectionView {
	return CanonicalConnectionView{
		Kind:         connection.Kind,
		Role:         connection.Role,
		DisplayName:  connection.DisplayName,
		Protocol:     connection.Protocol,
		Endpoint:     connection.Endpoint,
		TLS:          connection.TLS,
		Transport:    connection.Transport,
		GroupMembers: append([]string(nil), connection.GroupMembers...),
		Formats:      append([]string(nil), connection.Formats...),
		Adaptations:  append([]subcanonical.Adaptation(nil), connection.Adaptations...),
	}
}

func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}
