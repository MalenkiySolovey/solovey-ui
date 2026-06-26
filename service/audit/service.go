// Package audit records, queries, and prunes security and operations events.
package audit

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const (
	AuditSeverityInfo = "info"
	AuditSeverityWarn = "warn"
)

type Service struct {
	enqueue func(model.AuditEvent)
}

type Event struct {
	Actor     string
	Event     string
	Resource  string
	Severity  string
	IP        string
	UserAgent string
	Details   map[string]any
}

func New(enqueue func(model.AuditEvent)) Service {
	return Service{enqueue: enqueue}
}

func (s *Service) Record(event Event, synchronous bool) error {
	record, err := BuildRecord(event)
	if err != nil {
		return err
	}
	if synchronous {
		return WriteEvents([]model.AuditEvent{record})
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
		details["bind_error"] = bindErr.Error()
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
	if event.Severity == "" {
		event.Severity = AuditSeverityInfo
	}
	details, err := json.Marshal(redact.Value(event.Details))
	if err != nil {
		return model.AuditEvent{}, err
	}
	return model.AuditEvent{
		DateTime:  time.Now().Unix(),
		Actor:     event.Actor,
		Event:     event.Event,
		Resource:  event.Resource,
		Severity:  event.Severity,
		IP:        event.IP,
		UserAgent: event.UserAgent,
		Details:   details,
	}, nil
}

func WriteEvents(events []model.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	db := dbsqlite.DB()
	if db == nil {
		return errors.New("audit database is not initialized")
	}
	return db.Create(&events).Error
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
	query := dbsqlite.DB().Model(model.AuditEvent{})
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

func (s *Service) ListXUIImportReports(limit int) ([]model.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	events := make([]model.AuditEvent, 0, limit)
	err := dbsqlite.DB().
		Where("event IN ?", []string{"xui_import", "xui_import_failed", "xui_import_rollback"}).
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
