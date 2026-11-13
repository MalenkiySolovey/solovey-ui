//go:build !minimal

package service

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"gorm.io/gorm"
)

func HandleRestorePostOpen(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return DefaultRuntime.Shutdown(ctx)
	}
	if err := NormalizeRestoredData(ctx, db); err != nil {
		return err
	}
	return DefaultRuntime.RebuildContext(ctx, db)
}

// NormalizeRestoredData only changes the supplied database. Rehearsal invokes
// it on a disposable candidate and must never replace the live runtime's DB,
// publication snapshot or listeners.
func NormalizeRestoredData(ctx context.Context, db *gorm.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return errors.New("fallback-html restore database is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ReconcileLegacySelfSteal(ctx, db, time.Now().UTC()); err != nil {
		return err
	}
	migrator := db.Migrator()
	hasReservations := migrator.HasTable(&authority.ReservationModel{})
	if !migrator.HasTable(&fallbackdomain.Site{}) {
		if hasReservations {
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return authority.ReconcileRestoredInTx(ctx, tx, time.Now().UTC())
			}); err != nil {
				return err
			}
		}
		return nil
	}
	hasPublishes := migrator.HasTable(&fallbackdomain.Publish{})
	hasEvents := migrator.HasTable(&fallbackdomain.Event{})
	now := time.Now().Unix()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hasReservations {
			if err := authority.ReconcileRestoredInTx(ctx, tx, time.Now().UTC()); err != nil {
				return err
			}
		}
		if hasPublishes {
			if err := tx.Model(&fallbackdomain.Publish{}).Where("active = ?", true).Update("active", false).Error; err != nil {
				return err
			}
		}
		message := "restored from database backup; publish again to recreate verified public files"
		if err := tx.Model(&fallbackdomain.Site{}).Where("1 = 1").Updates(map[string]any{
			"status":     "draft",
			"last_error": message,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if !hasEvents {
			return nil
		}
		var sites []fallbackdomain.Site
		if err := tx.Select("id").Find(&sites).Error; err != nil {
			return err
		}
		for _, site := range sites {
			if err := recordEvent(&backendStore{db: tx}, site.ID, "system", "site_restore_deactivated", map[string]any{
				"reason": message,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}
