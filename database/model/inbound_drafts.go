package model

import "encoding/json"

type InboundDraft struct {
	Id          uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	Source      string          `json:"source" gorm:"size:128;not null;index:idx_inbound_drafts_source_ref"`
	SourceRef   string          `json:"sourceRef" gorm:"column:source_ref;size:160;not null;index:idx_inbound_drafts_source_ref"`
	Status      string          `json:"status" gorm:"size:32;not null;index"`
	InboundType string          `json:"inboundType" gorm:"column:inbound_type;size:64"`
	Tag         string          `json:"tag" gorm:"size:255"`
	Payload     json.RawMessage `json:"payload"`
	ReviewNotes json.RawMessage `json:"reviewNotes" gorm:"column:review_notes"`
	CreatedBy   string          `json:"createdBy" gorm:"column:created_by;size:128"`
	CreatedAt   int64           `json:"createdAt" gorm:"default:0;not null"`
	UpdatedAt   int64           `json:"updatedAt" gorm:"default:0;not null"`
	ExpiresAt   int64           `json:"expiresAt" gorm:"column:expires_at;default:0;not null;index"`
}

func (InboundDraft) TableName() string { return "inbound_drafts" }
