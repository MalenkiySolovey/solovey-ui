// Package backendobservation binds protection observations to the existing
// Backend storage admission and public observation registry.
package backendobservation

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/observationio"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

type Source struct{}

func (Source) SubscribeObservations(buffer int) (observationio.Subscription, func(), error) {
	return publicsurface.SubscribeObservations(buffer)
}

type store struct{ repository *repository.Repository }

func NewStore(repository *repository.Repository) observationio.Store {
	if repository == nil {
		return nil
	}
	return &store{repository: repository}
}

func (s *store) LoadSettings(ctx context.Context) (domain.Settings, bool, error) {
	return s.repository.LoadSettings(ctx)
}

func (s *store) BeginObservation(ctx context.Context) (observationio.Cycle, error) {
	admitted, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return nil, err
	}
	return &cycle{repository: s.repository, ctx: admitted, release: release}, nil
}

type cycle struct {
	repository *repository.Repository
	ctx        context.Context
	release    func()
	once       sync.Once
	closed     atomic.Bool
}

func (c *cycle) Context() context.Context { return c.ctx }
func (c *cycle) Close()                   { c.once.Do(func() { c.closed.Store(true); c.release() }) }
func (c *cycle) check() error {
	if c.closed.Load() {
		return observationio.ErrCycleClosed
	}
	return c.ctx.Err()
}
func (c *cycle) LoadSettings() (domain.Settings, bool, error) {
	if err := c.check(); err != nil {
		return domain.Settings{}, false, err
	}
	return c.repository.LoadSettings(c.ctx)
}
func (c *cycle) ActiveProfiles() ([]observationio.ProfileSnapshot, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	models, _, err := c.repository.ListProfiles(c.ctx, repository.ProfileFilter{PageQuery: repository.PageQuery{Page: 1, Limit: 1000}, Status: "active"})
	if err != nil {
		return nil, err
	}
	values := make([]observationio.ProfileSnapshot, 0, len(models))
	for _, m := range models {
		values = append(values, observationio.ProfileSnapshot{ResourceID: m.ResourceID, Enabled: m.Enabled, Mode: m.Mode,
			AcceptedFingerprint: m.AcceptedFingerprint, ScoreThreshold: m.ScoreThreshold, GraylistTTLSeconds: m.GraylistTTLSeconds})
	}
	return values, nil
}
func (c *cycle) ActiveIPAllowlist(now time.Time) ([]netip.Prefix, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	return c.repository.ActiveIPAllowlist(c.ctx, now)
}
func (c *cycle) LoadScore(key scoring.ScoreKey) (scoring.ScoreState, error) {
	if err := c.check(); err != nil {
		return scoring.ScoreState{}, err
	}
	value, err := c.repository.LoadScore(c.ctx, key)
	if errors.Is(err, repository.ErrScoreNotFound) {
		err = observationio.ErrScoreNotFound
	}
	return value, err
}
func (c *cycle) PersistObservationBatch(states []scoring.ScoreState, observations []events.ProbeEvent, clear []scoring.ScoreKey) error {
	if err := c.check(); err != nil {
		return err
	}
	return observationWriteError(c.repository.PersistObservationBatch(c.ctx, states, observations, clear))
}
func (c *cycle) Purge(policy events.RetentionPolicy) (events.PurgeResult, error) {
	if err := c.check(); err != nil {
		return events.PurgeResult{}, err
	}
	return c.repository.Purge(c.ctx, policy)
}

type busyWriteError struct{ cause error }

func (e *busyWriteError) Error() string        { return e.cause.Error() }
func (e *busyWriteError) Unwrap() error        { return e.cause }
func (e *busyWriteError) Is(target error) bool { return target == observationio.ErrWriteBusy }

func observationWriteError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy") || strings.Contains(message, "sqlite_busy") {
		return &busyWriteError{cause: err}
	}
	return err
}
