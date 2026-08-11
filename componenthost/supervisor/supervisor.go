package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentmigrations "github.com/MalenkiySolovey/solovey-ui/componenthost/migrations"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type Supervisor struct {
	host                componenthost.Deps
	installedComponents func() ([]registry.Component, error)
	activeComponents    func() ([]registry.Component, error)

	mu      sync.Mutex
	running []registry.Component
}

var componentMigrationMu sync.Mutex

func New(host componenthost.Deps) *Supervisor {
	return &Supervisor{
		host:                host,
		installedComponents: installedComponents,
		activeComponents:    activeComponents,
	}
}

func (s *Supervisor) Migrate(ctx context.Context) error {
	if s == nil {
		return nil
	}
	componentMigrationMu.Lock()
	defer componentMigrationMu.Unlock()
	var joined error
	if err := componentmigrations.EnsureJournal(dbsqlite.DB()); err != nil {
		return err
	}
	installedOwners, err := installstate.InstalledComponents()
	if err != nil {
		return err
	}
	for _, owner := range installedOwners {
		if _, available := registry.ComponentByID(owner.ID); !available {
			_ = componentmigrations.RecordUnavailableOwner(dbsqlite.DB(), owner)
			return fmt.Errorf("installed component owner %q is unavailable; migrations fail closed", owner.ID)
		}
	}
	components, err := s.installed()
	if err != nil {
		return err
	}
	for _, component := range components {
		step, err := componentmigrations.StepFor(component.Manifest)
		if err != nil {
			return err
		}
		run, err := componentmigrations.BeginStep(dbsqlite.DB(), step)
		if err != nil {
			return fmt.Errorf("component %q migration admission failed: %w", component.Manifest.ID, err)
		}
		if !run {
			continue
		}
		migrator, ok := component.Lifecycle.(lifecycle.Migrator)
		var migrationErr error
		if ok {
			migrationErr = safeCall(component.Manifest.ID, "migrate", func() error {
				return migrator.Migrate(ctx, lifecycle.Context{Host: s.host})
			})
		}
		if finishErr := componentmigrations.FinishStep(dbsqlite.DB(), step, migrationErr); finishErr != nil {
			joined = errors.Join(joined, finishErr)
		}
		if migrationErr != nil {
			joined = errors.Join(joined, migrationErr)
			continue
		}
		if err := componentmigrations.RecordApplied(dbsqlite.DB(), component.Manifest); err != nil {
			joined = errors.Join(joined, fmt.Errorf("component %q migration journal failed: %w", component.Manifest.ID, err))
		}
	}
	return joined
}

func (s *Supervisor) DropData(ctx context.Context, componentID string) error {
	if s == nil {
		return nil
	}
	if err := manifest.ValidateID(componentID); err != nil {
		return err
	}
	component, ok := registry.ComponentByID(componentID)
	if !ok {
		return fmt.Errorf("component %q is not available in this binary", componentID)
	}

	var joined error
	if dropper, ok := component.Lifecycle.(lifecycle.DataDropper); ok {
		if err := safeCall(component.Manifest.ID, "drop data", func() error {
			return dropper.DropData(ctx, lifecycle.Context{Host: s.host})
		}); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	if db := dbsqlite.DB(); db != nil {
		if err := componentmigrations.DeleteRecords(db, componentID); err != nil {
			joined = errors.Join(joined, fmt.Errorf("component %q migration journal cleanup failed: %w", componentID, err))
		}
	}
	return joined
}

func (s *Supervisor) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.Reconcile(ctx)
}

func (s *Supervisor) Reconcile(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var joined error
	active, err := s.active()
	if err != nil {
		return err
	}

	activeByID := make(map[string]registry.Component, len(active))
	for _, component := range active {
		activeByID[component.Manifest.ID] = component
	}

	runningByID := make(map[string]registry.Component, len(s.running))
	for _, component := range s.running {
		runningByID[component.Manifest.ID] = component
	}

	var stopFailed []registry.Component
	for i := len(s.running) - 1; i >= 0; i-- {
		component := s.running[i]
		if _, ok := activeByID[component.Manifest.ID]; ok {
			continue
		}
		if err := safeCall(component.Manifest.ID, "stop", func() error {
			return component.Lifecycle.Stop(ctx)
		}); err != nil {
			joined = errors.Join(joined, err)
			stopFailed = append(stopFailed, component)
		}
	}

	nextRunning := make([]registry.Component, 0, len(active)+len(stopFailed))
	for _, component := range active {
		if running, ok := runningByID[component.Manifest.ID]; ok {
			nextRunning = append(nextRunning, running)
			continue
		}
		if err := safeCall(component.Manifest.ID, "start", func() error {
			return component.Lifecycle.Start(ctx, lifecycle.Context{Host: s.host})
		}); err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		nextRunning = append(nextRunning, component)
	}
	nextRunning = append(nextRunning, stopFailed...)
	s.running = nextRunning
	state.InvalidateActiveCache()
	return joined
}

func (s *Supervisor) installed() ([]registry.Component, error) {
	if s.installedComponents == nil {
		return installedComponents()
	}
	return s.installedComponents()
}

func (s *Supervisor) active() ([]registry.Component, error) {
	if s.activeComponents == nil {
		return activeComponents()
	}
	return s.activeComponents()
}

func activeComponents() ([]registry.Component, error) {
	installed, err := installedComponents()
	if err != nil {
		return nil, err
	}
	available := make([]manifest.Manifest, 0, len(installed))
	for _, component := range installed {
		available = append(available, component.Manifest)
	}
	enabled, err := enabledstate.EnabledIDs(available)
	if err != nil {
		return nil, err
	}
	result := make([]registry.Component, 0, len(installed))
	for _, component := range installed {
		if _, ok := enabled[component.Manifest.ID]; ok {
			result = append(result, component)
		}
	}
	return result, nil
}

func installedComponents() ([]registry.Component, error) {
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	result := make([]registry.Component, 0, len(installed))
	for _, item := range installed {
		component, ok := registry.ComponentByID(item.ID)
		if !ok {
			return nil, fmt.Errorf("installed component owner %q is unavailable", item.ID)
		}
		if item.Delivery != "" && item.Delivery != component.Manifest.Delivery {
			return nil, fmt.Errorf("installed component %q delivery %q does not match binary delivery %q", item.ID, item.Delivery, component.Manifest.Delivery)
		}
		result = append(result, component)
	}
	return result, nil
}

func (s *Supervisor) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var joined error
	for i := len(s.running) - 1; i >= 0; i-- {
		component := s.running[i]
		if err := safeCall(component.Manifest.ID, "stop", func() error {
			return component.Lifecycle.Stop(ctx)
		}); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	s.running = nil
	return joined
}

func safeCall(componentID string, action string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("component %q %s panic: %v", componentID, action, recovered)
		}
	}()
	if err := call(); err != nil {
		return fmt.Errorf("component %q %s failed: %w", componentID, action, err)
	}
	return nil
}
