package service

import (
	"context"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
)

// Caller owns ConfigService's established lifecycle serializer. A deliberate
// hold also reconciles an already-running core after restored desired state.
func (s *ConfigService) maintenanceHeldLocked() (bool, error) {
	return s.maintenanceHeldLockedContext(context.Background())
}

func (s *ConfigService) maintenanceHeldLockedContext(ctx context.Context) (bool, error) {
	db := s.SettingService.settingDatabase()
	if db == nil {
		return false, ErrMaintenanceUnavailable
	}
	settings := SettingService{database: db.WithContext(ctx)}
	held, err := settings.CoreMaintenance()
	if err != nil {
		return false, err
	}
	if held && s.IsCoreRunning() {
		return true, s.stopCoreLocked()
	}
	return held, nil
}

// Desired state is committed before lifecycle effects. Failed stop/start is
// reported without disguising or rolling back the operator's durable intent.
func (s *ConfigService) SetCoreMaintenance(ctx context.Context, generation string, held bool) error {
	if s.coreInstance() == nil || s.runtime().restart() == nil {
		return coreruntime.ErrCoreUnavailable
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.runtime().restart().RunBlockingContext(ctx, func() error {
		status := s.coreInstance().RuntimeStatus(ctx)
		if generation != status.Generation {
			return coreruntime.ErrStaleGeneration
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		value := "false"
		if held {
			value = "true"
		}
		db := s.SettingService.settingDatabase()
		if db == nil {
			return ErrMaintenanceUnavailable
		}
		settings := SettingService{database: db.WithContext(ctx)}
		if err := settings.setCoreMaintenanceValue(value); err != nil {
			return ErrMaintenanceUnavailable
		}
		realtime.Publish(realtime.TopicCoreState, map[string]any{"maintenance": held})
		if held {
			return s.stopCoreLocked()
		}
		return s.startCoreLockedContext(ctx, true)
	})
}
