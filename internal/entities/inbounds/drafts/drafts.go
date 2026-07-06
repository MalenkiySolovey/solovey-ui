package drafts

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

const (
	StatusBlocked        = "blocked"
	StatusReviewRequired = "review_required"
	StatusApplied        = "applied"
	StatusDiscarded      = "discarded"
)

type CreateInput struct {
	Source      string
	SourceRef   string
	Status      string
	InboundType string
	Tag         string
	Payload     json.RawMessage
	ReviewNotes json.RawMessage
	CreatedBy   string
	ExpiresAt   int64
	Now         int64
}

func ListOpen(tx *gorm.DB, now int64) ([]model.InboundDraft, error) {
	if tx == nil {
		return nil, common.NewError("database is not initialized")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	if err := CleanupExpired(tx, now); err != nil {
		return nil, err
	}
	var rows []model.InboundDraft
	err := tx.
		Where("status IN ?", []string{StatusBlocked, StatusReviewRequired}).
		Order("id DESC").
		Limit(100).
		Find(&rows).Error
	return rows, err
}

func Create(tx *gorm.DB, input CreateInput) (model.InboundDraft, error) {
	var draft model.InboundDraft
	if tx == nil {
		return draft, common.NewError("database is not initialized")
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		return draft, common.NewError("inbound draft source is required")
	}
	sourceRef := strings.TrimSpace(input.SourceRef)
	if sourceRef == "" {
		return draft, common.NewError("inbound draft source reference is required")
	}
	status := strings.TrimSpace(input.Status)
	if !validStatus(status) {
		return draft, fmt.Errorf("unsupported inbound draft status %q", input.Status)
	}
	payload, err := normalizedJSON(input.Payload, "payload")
	if err != nil {
		return draft, err
	}
	reviewNotes, err := normalizedJSON(input.ReviewNotes, "review notes")
	if err != nil {
		return draft, err
	}
	now := input.Now
	if now == 0 {
		now = time.Now().Unix()
	}
	draft = model.InboundDraft{
		Source:      source,
		SourceRef:   sourceRef,
		Status:      status,
		InboundType: strings.TrimSpace(input.InboundType),
		Tag:         strings.TrimSpace(input.Tag),
		Payload:     payload,
		ReviewNotes: reviewNotes,
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   input.ExpiresAt,
	}
	return draft, tx.Create(&draft).Error
}

func CleanupExpired(tx *gorm.DB, now int64) error {
	if tx == nil {
		return common.NewError("database is not initialized")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	return tx.
		Where("expires_at > 0 AND expires_at < ? AND status IN ?", now, []string{StatusBlocked, StatusReviewRequired}).
		Delete(&model.InboundDraft{}).Error
}

func MarkApplied(tx *gorm.DB, id uint, now int64) error {
	return markClosed(tx, id, StatusApplied, now)
}

func MarkDiscarded(tx *gorm.DB, id uint, now int64) error {
	return markClosed(tx, id, StatusDiscarded, now)
}

func markClosed(tx *gorm.DB, id uint, status string, now int64) error {
	if tx == nil {
		return common.NewError("database is not initialized")
	}
	if id == 0 {
		return common.NewError("inbound draft id is required")
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	result := tx.Model(&model.InboundDraft{}).
		Where("id = ? AND status IN ?", id, []string{StatusBlocked, StatusReviewRequired}).
		Updates(map[string]any{"status": status, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("inbound draft %d is not open", id)
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case StatusBlocked, StatusReviewRequired, StatusApplied, StatusDiscarded:
		return true
	default:
		return false
	}
}

func normalizedJSON(raw json.RawMessage, label string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("inbound draft %s must be valid JSON", label)
	}
	return append(json.RawMessage(nil), raw...), nil
}
