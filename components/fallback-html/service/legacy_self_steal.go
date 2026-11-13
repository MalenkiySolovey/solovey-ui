//go:build !minimal

package service

import (
	"context"
	"encoding/json"
	"errors"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	inbounddrafts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/drafts"
	"gorm.io/gorm"
	"time"
)

// ReconcileLegacySelfSteal retires only exact historical self-steal records.
// Payloads remain byte-for-byte in their owning tables; targets, publishes,
// TLS rows, inbounds, reservations, and runtime state are never touched.
func ReconcileLegacySelfSteal(ctx context.Context, db *gorm.DB, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	hasHistorical := db.Migrator().HasTable(&fallbackdomain.SelfStealDraft{})
	hasCore := db.Migrator().HasTable(&model.InboundDraft{})
	if !hasHistorical && !hasCore {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hasHistorical {
			var rows []fallbackdomain.SelfStealDraft
			if err := tx.WithContext(ctx).
				Where("status IN ?", []string{"blocked", "ready"}).
				Order("id ASC").Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return err
				}
				status := LegacySelfStealRetiredStatus
				if _, err := DecodeLegacySelfStealPayload(row.Payload); err != nil {
					status = LegacySelfStealInvalidStatus
				}
				if err := tx.WithContext(ctx).Model(&fallbackdomain.SelfStealDraft{}).
					Where("id = ? AND status IN ?", row.ID, []string{"blocked", "ready"}).
					Update("status", status).Error; err != nil {
					return err
				}
			}
		}
		if !hasCore {
			return nil
		}
		var drafts []model.InboundDraft
		if err := tx.WithContext(ctx).
			Where("source = ? AND status IN ?", LegacySelfStealSource,
				[]string{inbounddrafts.StatusBlocked, inbounddrafts.StatusReviewRequired}).
			Order("id ASC").Find(&drafts).Error; err != nil {
			return err
		}
		for _, draft := range drafts {
			if err := ctx.Err(); err != nil {
				return err
			}
			inspection, decodeErr := DecodeLegacySelfStealPayload(draft.Payload)
			notes, err := json.Marshal(struct {
				Classification string   `json:"classification"`
				ReasonCodes    []string `json:"reasonCodes"`
			}{Classification: inspection.Classification, ReasonCodes: inspection.ReasonCodes})
			if err != nil {
				return err
			}
			if decodeErr != nil && len(inspection.ReasonCodes) == 0 {
				return errors.New(LegacySelfStealInvalidReason)
			}
			if err := tx.WithContext(ctx).Model(&model.InboundDraft{}).
				Where("id = ? AND source = ? AND status IN ?", draft.Id, LegacySelfStealSource,
					[]string{inbounddrafts.StatusBlocked, inbounddrafts.StatusReviewRequired}).
				Updates(map[string]any{
					"status":       inbounddrafts.StatusDiscarded,
					"review_notes": notes,
					"updated_at":   now.Unix(),
				}).Error; err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}
