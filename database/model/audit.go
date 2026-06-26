package model

import "encoding/json"

type Changes struct {
	Id       uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	DateTime int64           `json:"dateTime"`
	Actor    string          `json:"actor"`
	Key      string          `json:"key"`
	Action   string          `json:"action"`
	Obj      json.RawMessage `json:"obj"`
}

type AuditEvent struct {
	Id        uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	DateTime  int64           `json:"dateTime" gorm:"index"`
	Actor     string          `json:"actor" gorm:"index"`
	Event     string          `json:"event" gorm:"index"`
	Resource  string          `json:"resource"`
	Severity  string          `json:"severity" gorm:"index"`
	IP        string          `json:"ip"`
	UserAgent string          `json:"userAgent"`
	Details   json.RawMessage `json:"details"`
}
