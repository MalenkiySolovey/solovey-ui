//go:build !minimal

package remote

import "encoding/json"

type RemoteOutboundSubscription struct {
	Id                 uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder          int             `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Name               string          `json:"name" form:"name"`
	Url                string          `json:"url" form:"url" gorm:"column:url"`
	Enabled            bool            `json:"enabled" form:"enabled" gorm:"not null"`
	TagPrefix          string          `json:"tagPrefix" form:"tagPrefix"`
	AutoUpdate         bool            `json:"autoUpdate" form:"autoUpdate" gorm:"column:auto_update;default:false;not null"`
	UpdateInterval     int64           `json:"updateInterval" form:"updateInterval" gorm:"column:update_interval;default:86400;not null"`
	LastUpdated        int64           `json:"lastUpdated" form:"lastUpdated" gorm:"default:0;not null"`
	LastError          string          `json:"lastError" form:"lastError"`
	CanonicalSnapshot  json.RawMessage `json:"-" form:"-" gorm:"column:canonical_snapshot"`
	CollectionSnapshot json.RawMessage `json:"-" form:"-" gorm:"column:collection_snapshot"`
	CreatedAt          int64           `json:"createdAt" form:"createdAt" gorm:"default:0;not null"`
	UpdatedAt          int64           `json:"updatedAt" form:"updatedAt" gorm:"default:0;not null"`

	Groups      []RemoteOutboundGroup      `json:"groups,omitempty" gorm:"foreignKey:SubscriptionId;constraint:OnDelete:CASCADE"`
	Connections []RemoteOutboundConnection `json:"connections,omitempty" gorm:"foreignKey:SubscriptionId;constraint:OnDelete:CASCADE"`
}

func (RemoteOutboundSubscription) TableName() string {
	return "remote_outbound_subscriptions"
}

type RemoteOutboundGroup struct {
	Id              uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SubscriptionId  uint   `json:"subscriptionId" form:"subscriptionId" gorm:"column:subscription_id;index;not null"`
	SortOrder       int    `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Name            string `json:"name" form:"name"`
	Enabled         bool   `json:"enabled" form:"enabled" gorm:"not null"`
	OutboundEnabled bool   `json:"outboundEnabled" form:"outboundEnabled" gorm:"column:outbound_enabled;default:false;not null"`
	CreatedAt       int64  `json:"createdAt" form:"createdAt" gorm:"default:0;not null"`
	UpdatedAt       int64  `json:"updatedAt" form:"updatedAt" gorm:"default:0;not null"`
}

func (RemoteOutboundGroup) TableName() string {
	return "remote_outbound_groups"
}

type RemoteOutboundGroupConnection struct {
	Id           uint  `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	GroupId      uint  `json:"groupId" form:"groupId" gorm:"column:group_id;index:idx_remote_outbound_group_connection,unique;not null"`
	ConnectionId uint  `json:"connectionId" form:"connectionId" gorm:"column:connection_id;index:idx_remote_outbound_group_connection,unique;not null"`
	CreatedAt    int64 `json:"createdAt" form:"createdAt" gorm:"default:0;not null"`
}

func (RemoteOutboundGroupConnection) TableName() string {
	return "remote_outbound_group_connections"
}

type RemoteOutboundConnection struct {
	Id             uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SubscriptionId uint            `json:"subscriptionId" form:"subscriptionId" gorm:"column:subscription_id;index:idx_remote_outbound_source,unique;not null"`
	GroupId        uint            `json:"groupId" form:"groupId" gorm:"column:group_id;index"`
	GroupIds       []uint          `json:"groupIds,omitempty" form:"groupIds" gorm:"-"`
	SortOrder      int             `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Name           string          `json:"name" form:"name"`
	SourceKey      string          `json:"sourceKey" form:"sourceKey" gorm:"column:source_key;index:idx_remote_outbound_source,unique;not null"`
	Type           string          `json:"type" form:"type"`
	SourceType     string          `json:"sourceType,omitempty" form:"-" gorm:"-"`
	ConvertedType  string          `json:"convertedType,omitempty" form:"-" gorm:"-"`
	OutboundTag    string          `json:"outboundTag" form:"outboundTag" gorm:"column:outbound_tag;uniqueIndex;not null"`
	Enabled        bool            `json:"enabled" form:"enabled" gorm:"not null"`
	Synced         bool            `json:"synced" form:"synced" gorm:"default:false;not null"`
	OutboundId     *uint           `json:"outboundId,omitempty" form:"outboundId" gorm:"column:outbound_id;index"`
	Options        json.RawMessage `json:"options" form:"options"`
	Canonical      json.RawMessage `json:"-" form:"-" gorm:"column:canonical"`
	LastSeen       int64           `json:"lastSeen" form:"lastSeen" gorm:"default:0;not null"`
	CreatedAt      int64           `json:"createdAt" form:"createdAt" gorm:"default:0;not null"`
	UpdatedAt      int64           `json:"updatedAt" form:"updatedAt" gorm:"default:0;not null"`
}

func (RemoteOutboundConnection) TableName() string {
	return "remote_outbound_connections"
}
