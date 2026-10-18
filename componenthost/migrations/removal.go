package migrations

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
)

// RemovalBlocker keeps unfinished/failed migration ownership with the installed
// component. Applied journal rows are history, not remaining owner data.
func RemovalBlocker(ctx context.Context, db *gorm.DB, id string) string {
	if ctx == nil || ctx.Err() != nil || db == nil || manifest.ValidateID(id) != nil {
		return "component_migration_state_unavailable"
	}
	var tables int64
	if err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", (model.MigrationJournal{}).TableName()).Scan(&tables).Error; err != nil {
		return "component_migration_state_unavailable"
	}
	if tables == 0 {
		return ""
	}
	var count int64
	err := db.WithContext(ctx).Model(&model.MigrationJournal{}).
		Where("scope = ? AND owner_id = ? AND (UPPER(COALESCE(state, '')) <> ? OR UPPER(COALESCE(compatibility_state, '')) <> ?)", ScopeComponent, id, "APPLIED", "COMPATIBLE").Count(&count).Error
	if err != nil {
		return "component_migration_state_unavailable"
	}
	if count != 0 {
		return "component_migration_recovery_required"
	}
	return ""
}
