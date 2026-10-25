// Package audit records, queries, and prunes security and operations events.
package audit

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/auditrecord"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	AuditSeverityInfo = auditrecord.SeverityInfo
	AuditSeverityWarn = auditrecord.SeverityWarn

	AuditActorMaxBytes     = auditrecord.ActorMaxBytes
	AuditEventMaxBytes     = auditrecord.EventMaxBytes
	AuditResourceMaxBytes  = auditrecord.ResourceMaxBytes
	AuditSeverityMaxBytes  = auditrecord.SeverityMaxBytes
	AuditIPMaxBytes        = auditrecord.IPMaxBytes
	AuditUserAgentMaxBytes = auditrecord.UserAgentMaxBytes
	AuditDetailsMaxBytes   = auditrecord.DetailsMaxBytes
	AuditWriteTimeout      = 3 * time.Second
)

type Service struct {
	enqueue    func(model.AuditEvent)
	aggregator *DenialAggregator
}

type Event = auditrecord.Event

func New(enqueue func(model.AuditEvent), aggregators ...*DenialAggregator) Service {
	var aggregator *DenialAggregator
	if len(aggregators) > 0 {
		aggregator = aggregators[0]
	}
	return Service{enqueue: enqueue, aggregator: aggregator}
}

func (s *Service) Record(event Event, synchronous bool) error {
	return s.RecordContext(context.Background(), event, synchronous)
}

func (s *Service) RecordContext(ctx context.Context, event Event, synchronous bool) error {
	if !synchronous && s != nil && s.aggregator != nil {
		emit, count := s.aggregator.Observe(event, time.Now())
		if !emit {
			return nil
		}
		if HighFrequencyDenial(event.Event) {
			details := make(map[string]any, len(event.Details)+2)
			for key, value := range event.Details {
				details[key] = value
			}
			details["aggregationCount"] = count
			details["aggregationWindowSeconds"] = int64(DefaultDenialAggregationWindow / time.Second)
			event.Details = details
		}
	}
	record, err := BuildRecord(event)
	if err != nil {
		return err
	}
	if synchronous {
		if ctx == nil {
			return errors.New("audit write context is unavailable")
		}
		writeCtx, cancel := context.WithTimeout(ctx, AuditWriteTimeout)
		defer cancel()
		return WriteEventsContext(writeCtx, []model.AuditEvent{record})
	}
	if s != nil && s.enqueue != nil {
		s.enqueue(record)
	}
	return nil
}

func (s *Service) RecordListenFallback(component, requestedAddr, fallbackAddr string, bindErr error, synchronous bool) error {
	details := map[string]any{
		"component":      component,
		"requested_addr": requestedAddr,
		"fallback_addr":  fallbackAddr,
	}
	if bindErr != nil {
		details["reason"] = "listen_failed"
	}
	err := s.Record(Event{
		Actor:    "system",
		Event:    "listen_fallback",
		Resource: "network",
		Severity: AuditSeverityWarn,
		Details:  details,
	}, synchronous)
	if err != nil {
		logger.Warning("listen fallback audit failed:", err)
	}
	return err
}

func BuildRecord(event Event) (model.AuditEvent, error) {
	return auditrecord.Build(event)
}

func WriteEvents(events []model.AuditEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), AuditWriteTimeout)
	defer cancel()
	return WriteEventsContext(ctx, events)
}

func WriteEventsContext(ctx context.Context, events []model.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	if ctx == nil {
		return errors.New("audit write context is unavailable")
	}
	db := dbsqlite.DB()
	if db == nil {
		return errors.New("audit database is not initialized")
	}
	return db.WithContext(ctx).Create(&events).Error
}

func (s *Service) List(limit int) ([]model.AuditEvent, error) {
	events, _, err := s.ListPage(0, limit)
	return events, err
}

func (s *Service) ListPage(cursor uint64, limit int) ([]model.AuditEvent, uint64, error) {
	return s.ListPageFiltered(cursor, limit, "", "", 0, 0)
}

func (s *Service) ListPageFiltered(cursor uint64, limit int, event string, severity string, since int64, until int64) ([]model.AuditEvent, uint64, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 200 {
		limit = 200
	}
	events := make([]model.AuditEvent, 0, limit+1)
	db := dbsqlite.DB()
	if db == nil {
		return nil, 0, errors.New("audit database is not initialized")
	}
	query := db.Model(model.AuditEvent{})
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	if event != "" {
		query = query.Where("event = ?", event)
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if since > 0 {
		query = query.Where("date_time >= ?", since)
	}
	if until > 0 {
		query = query.Where("date_time <= ?", until)
	}
	err := query.
		Order("id desc").
		Limit(limit + 1).
		Find(&events).Error
	if err != nil {
		return nil, 0, err
	}
	var nextCursor uint64
	if len(events) > limit {
		events = events[:limit]
		nextCursor = events[len(events)-1].Id
	}
	return events, nextCursor, nil
}

func (s *Service) ListByEvents(limit int, eventNames []string) ([]model.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(eventNames) == 0 {
		return []model.AuditEvent{}, nil
	}
	events := make([]model.AuditEvent, 0, limit)
	db := dbsqlite.DB()
	if db == nil {
		return nil, errors.New("audit database is not initialized")
	}
	err := db.
		Where("event IN ?", eventNames).
		Order("date_time desc").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (s *Service) Prune(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	before := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	_, err := s.PruneOlderThan(before)
	return err
}

func (s *Service) PruneOlderThan(before int64) (int64, error) {
	db := dbsqlite.DB()
	if db == nil {
		return 0, errors.New("audit database is not initialized")
	}
	result := db.Where("date_time < ?", before).Delete(&model.AuditEvent{})
	return result.RowsAffected, result.Error
}
