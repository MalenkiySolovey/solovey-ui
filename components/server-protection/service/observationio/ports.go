// Package observationio defines the bounded data and observation capabilities
// consumed by the protection worker. Implementations retain storage admission.
package observationio

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/MalenkiySolovey/solovey-host/componenthost/publicsurface"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

var ErrScoreNotFound = errors.New("protection observation score not found")
var ErrCycleClosed = errors.New("protection observation cycle is closed")
var ErrWriteBusy = errors.New("protection observation write is busy")

// ProfileSnapshot contains only the fields used for observation decisions.
type ProfileSnapshot struct {
	ResourceID          string
	Enabled             bool
	Mode                string
	AcceptedFingerprint string
	ScoreThreshold      int
	GraylistTTLSeconds  int
}

type Store interface {
	LoadSettings(context.Context) (domain.Settings, bool, error)
	BeginObservation(context.Context) (Cycle, error)
}

// Cycle retains the owner's admitted operation context across the exact batch.
// It exposes neither a database nor a transaction. Close ends its finite lease;
// its context cannot be reused to bypass the storage owner's generation fence.
type Cycle interface {
	Context() context.Context
	LoadSettings() (domain.Settings, bool, error)
	ActiveProfiles() ([]ProfileSnapshot, error)
	ActiveIPAllowlist(time.Time) ([]netip.Prefix, error)
	LoadScore(scoring.ScoreKey) (scoring.ScoreState, error)
	PersistObservationBatch([]scoring.ScoreState, []events.ProbeEvent, []scoring.ScoreKey) error
	Purge(events.RetentionPolicy) (events.PurgeResult, error)
	Close()
}

type Subscription interface {
	Observations() <-chan publicsurface.Observation
	Dropped() uint64
	Pending() int
}

type Source interface {
	SubscribeObservations(int) (Subscription, func(), error)
}
