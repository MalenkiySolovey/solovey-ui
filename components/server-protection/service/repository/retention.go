package repository

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"gorm.io/gorm"
)

func (r *Repository) Purge(ctx context.Context, policy events.RetentionPolicy) (events.PurgeResult, error) {
	if r == nil || r.db == nil {
		return events.PurgeResult{}, errors.New("server-protection repository is not initialized")
	}
	if err := policy.Validate(); err != nil {
		return events.PurgeResult{}, err
	}
	result := events.PurgeResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		remove := map[uint]struct{}{}
		if !policy.OlderThan.IsZero() {
			var ids []uint
			if err := tx.Model(&ProbeEventModel{}).Where("observed_at < ?", policy.OlderThan.Unix()).Pluck("id", &ids).Error; err != nil {
				return err
			}
			for _, id := range ids {
				remove[id] = struct{}{}
			}
		}
		var excess []uint
		if err := tx.Model(&ProbeEventModel{}).Order("observed_at DESC, id DESC").Offset(policy.GlobalLimit).Pluck("id", &excess).Error; err != nil {
			return err
		}
		for _, id := range excess {
			remove[id] = struct{}{}
		}
		var resources []string
		if err := tx.Model(&ProbeEventModel{}).Distinct("resource_id").Pluck("resource_id", &resources).Error; err != nil {
			return err
		}
		for _, resourceID := range resources {
			var ids []uint
			if err := tx.Model(&ProbeEventModel{}).Where("resource_id = ?", resourceID).
				Order("observed_at DESC, id DESC").Offset(policy.PerResourceLimit).Pluck("id", &ids).Error; err != nil {
				return err
			}
			for _, id := range ids {
				remove[id] = struct{}{}
			}
		}
		if len(remove) > 0 {
			ids := make([]uint, 0, len(remove))
			for id := range remove {
				ids = append(ids, id)
			}
			deleted := tx.Where("id IN ?", ids).Delete(&ProbeEventModel{})
			if deleted.Error != nil {
				return deleted.Error
			}
			result.EventsRemoved = int(deleted.RowsAffected)
		}
		expiredBefore := time.Now().Unix()
		deletedScores := tx.Where("expires_at IS NOT NULL AND expires_at < ?", expiredBefore).Delete(&ScoreStateModel{})
		if deletedScores.Error != nil {
			return deletedScores.Error
		}
		result.ScoresRemoved = int(deletedScores.RowsAffected)
		if err := tx.Where("expires_at < ?", expiredBefore).Delete(&GraylistModel{}).Error; err != nil {
			return err
		}
		if _, err := expireGraylistStatesV2(ctx, tx, time.Unix(expiredBefore, 0).UTC(), 1000); err != nil {
			return err
		}
		return nil
	})
	return result, err
}
