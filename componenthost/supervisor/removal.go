package supervisor

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

// TryAdmitRemoval excludes Start and migrations while the installed-state owner
// performs its final trusted read/metadata commit. Resource/feature inspectors
// must run before this fence; it is not a generic cleanup callback.
func (s *Supervisor) TryAdmitRemoval(ctx context.Context, id string, commit func() error) error {
	if s == nil || ctx == nil || commit == nil {
		return errors.New("component removal lifecycle boundary is unavailable")
	}
	if err := manifest.ValidateID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !componentMigrationMu.TryLock() {
		return errors.New("component migration is busy")
	}
	defer componentMigrationMu.Unlock()
	if !s.mu.TryLock() {
		return errors.New("component lifecycle is busy")
	}
	defer s.mu.Unlock()
	for _, component := range s.running {
		if component.Manifest.ID == id {
			return errors.New("component runtime instance is retained")
		}
	}
	observation, current := s.observations.Snapshot(id)
	if !current || observation.InstanceRetention == state.InstanceRetained || observation.Phase == state.LifecycleStartFailed {
		return errors.New("component runtime instance state is unresolved")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return commit()
}
