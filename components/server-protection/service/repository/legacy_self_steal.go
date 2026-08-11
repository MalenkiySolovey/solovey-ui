package repository

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
)

const (
	LegacySelfStealRequiresRepreview = "legacy_self_steal_requires_repreview"
	LegacyTopologyUnmanagedExisting  = "UNMANAGED_EXISTING"
)

// ReconcileLegacySelfStealProfiles converts historical intent into a disabled
// inspection-only migration candidate. It deliberately creates no native
// plan, state, operation, reservation, checkpoint, or provider authority.
func ReconcileLegacySelfStealProfiles(ctx context.Context, db *gorm.DB, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || !db.Migrator().HasTable(&ProfileModel{}) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&ProfileModel{}).
			Where("mode = ?", string(domain.ProfileModeSelfSteal)).
			Updates(map[string]any{
				"mode":                  string(domain.ProfileModeNativeFallback),
				"enabled":               false,
				"status":                "disabled",
				"migration_candidate":   string(domain.ProfileModeNativeFallback),
				"migration_reason":      LegacySelfStealRequiresRepreview,
				"legacy_topology_state": LegacyTopologyUnmanagedExisting,
				"inbound_tag":           "",
				"fallback_resource_id":  "",
				"public_listen":         "",
				"public_port":           nil,
				"handshake_host":        "",
				"handshake_target_host": "",
				"handshake_target_port": nil,
				"managed_firewall":      false,
				"updated_at":            now.Unix(),
			}).Error
	})
}
