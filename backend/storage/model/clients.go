package model

import "encoding/json"

// These reasons describe the existing Client enable bit; they are not a
// second access-policy owner. Only QUOTA_EXHAUSTED may be cleared by a
// scheduled traffic reset. Unknown future reasons remain blocking.
const (
	ClientDisabledManual  = "MANUAL_DISABLED"
	ClientDisabledQuota   = "QUOTA_EXHAUSTED"
	ClientDisabledExpired = "EXPIRED"
	ClientDisabledLegacy  = "LEGACY_DISABLED"
)

type Client struct {
	Id                  uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SortOrder           int             `json:"sortOrder" form:"sortOrder" gorm:"column:sort_order;default:0;not null;index"`
	Enable              bool            `json:"enable" form:"enable"`
	AccessDisableReason string          `json:"-" form:"-" gorm:"default:'';not null"`
	Name                string          `json:"name" form:"name"`
	SubSecret           string          `json:"subSecret,omitempty" form:"subSecret"`
	Config              json.RawMessage `json:"config,omitempty" form:"config"`
	Inbounds            json.RawMessage `json:"inbounds" form:"inbounds"`
	Links               json.RawMessage `json:"links,omitempty" form:"links"`
	Volume              int64           `json:"volume" form:"volume"`
	Expiry              int64           `json:"expiry" form:"expiry"`
	Down                int64           `json:"down" form:"down"`
	Up                  int64           `json:"up" form:"up"`
	Desc                string          `json:"desc" form:"desc"`
	Group               string          `json:"group" form:"group"`
	LimitIP             int             `json:"limitIp" form:"limitIp" gorm:"default:0;not null"`
	IPLimitMode         string          `json:"ipLimitMode" form:"ipLimitMode" gorm:"default:monitor;not null"`
	LastOnline          int64           `json:"lastOnline" form:"lastOnline" gorm:"default:0;not null"`
	LastIPCount         int             `json:"lastIpCount" form:"lastIpCount" gorm:"default:0;not null"`
	DelayStart          bool            `json:"delayStart" form:"delayStart" gorm:"default:false;not null"`
	AutoReset           bool            `json:"autoReset" form:"autoReset" gorm:"default:false;not null"`
	ResetDays           int             `json:"resetDays" form:"resetDays" gorm:"default:0;not null"`
	NextReset           int64           `json:"nextReset" form:"nextReset" gorm:"default:0;not null"`
	TotalUp             int64           `json:"totalUp" form:"totalUp" gorm:"default:0;not null"`
	TotalDown           int64           `json:"totalDown" form:"totalDown" gorm:"default:0;not null"`
}
