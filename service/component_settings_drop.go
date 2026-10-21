package service

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
)

// DropOwnedComponentSettings is the settings owner's lifecycle operation. The
// durable manifest remains authoritative even after the runtime is stopped and
// its settings contribution has been unregistered.
func (s *SettingService) DropOwnedComponentSettings(ctx context.Context, owner string) error {
	item, ok := durableowner.Lookup(owner)
	if !ok || !item.Database.Declared() {
		return errors.New("component settings owner is unavailable")
	}
	keys := append(append([]string(nil), item.Database.Settings...), item.Database.Secrets...)
	if len(keys) == 0 {
		return errors.New("component declares no owned settings")
	}
	db := s.settingDatabase()
	if db == nil {
		return ErrMaintenanceUnavailable
	}
	return db.WithContext(ctx).Where("key IN ?", keys).Delete(&model.Setting{}).Error
}
