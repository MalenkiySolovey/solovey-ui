package events

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

type ProbeEvent struct {
	ResourceID   string                `json:"resourceId"`
	ResourceKind domain.ResourceKind   `json:"resourceKind"`
	SourcePrefix string                `json:"sourcePrefix,omitempty"`
	IPFamily     int                   `json:"ipFamily,omitempty"`
	SignalKind   domain.SignalKind     `json:"signalKind"`
	ScoreDelta   int                   `json:"scoreDelta"`
	Action       domain.DecisionAction `json:"action"`
	SafeMeta     domain.SafeMeta       `json:"safeMeta"`
	ObservedAt   time.Time             `json:"observedAt"`
	DedupeKey    string                `json:"dedupeKey"`
}

type RetentionPolicy struct {
	GlobalLimit      int
	PerResourceLimit int
	OlderThan        time.Time
}

func (value RetentionPolicy) Validate() error {
	if value.GlobalLimit < 100 || value.GlobalLimit > 100000 {
		return errors.New("global event retention must be between 100 and 100000")
	}
	if value.PerResourceLimit < 20 || value.PerResourceLimit > 10000 || value.PerResourceLimit > value.GlobalLimit {
		return errors.New("per-resource event retention is invalid")
	}
	return nil
}

type PurgeResult struct {
	EventsRemoved int `json:"eventsRemoved"`
	ScoresRemoved int `json:"scoresRemoved"`
}

type EventStore interface {
	AppendBatch(ctx context.Context, events []ProbeEvent) error
	LoadScore(ctx context.Context, key scoring.ScoreKey) (scoring.ScoreState, error)
	SaveScore(ctx context.Context, state scoring.ScoreState) error
	Purge(ctx context.Context, policy RetentionPolicy) (PurgeResult, error)
}
