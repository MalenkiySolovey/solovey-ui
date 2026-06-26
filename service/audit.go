package service

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	auditsvc "github.com/MalenkiySolovey/solovey-ui/service/audit"
)

const (
	AuditSeverityInfo = auditsvc.AuditSeverityInfo
	AuditSeverityWarn = auditsvc.AuditSeverityWarn
)

type AuditEvent auditsvc.Event

type AuditService struct {
	Runtime *Runtime
}

func (s *AuditService) backend() auditsvc.Service {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return auditsvc.New(func(event model.AuditEvent) {
		writeAuditRuntime(runtime.audit(), event)
	})
}

func (s *AuditService) Record(event AuditEvent) error {
	backend := s.backend()
	return backend.Record(auditsvc.Event(event), false)
}

func (s *AuditService) RecordListenFallback(component, requestedAddr, fallbackAddr string, bindErr error) error {
	backend := s.backend()
	return backend.RecordListenFallback(component, requestedAddr, fallbackAddr, bindErr, false)
}

func (s *AuditService) List(limit int) ([]model.AuditEvent, error) {
	backend := s.backend()
	return backend.List(limit)
}

func (s *AuditService) ListPage(cursor uint64, limit int) ([]model.AuditEvent, uint64, error) {
	backend := s.backend()
	return backend.ListPage(cursor, limit)
}

func (s *AuditService) ListPageFiltered(cursor uint64, limit int, event, severity string, since, until int64) ([]model.AuditEvent, uint64, error) {
	backend := s.backend()
	return backend.ListPageFiltered(cursor, limit, event, severity, since, until)
}

func (s *AuditService) ListXUIImportReports(limit int) ([]model.AuditEvent, error) {
	backend := s.backend()
	return backend.ListXUIImportReports(limit)
}

func (s *AuditService) Prune(retentionDays int) error {
	backend := s.backend()
	return backend.Prune(retentionDays)
}

func (s *AuditService) PruneOlderThan(before int64) (int64, error) {
	backend := s.backend()
	return backend.PruneOlderThan(before)
}

func buildAuditRecord(event AuditEvent) (model.AuditEvent, error) {
	return auditsvc.BuildRecord(auditsvc.Event(event))
}

func writeAuditEvents(events []model.AuditEvent) error {
	return auditsvc.WriteEvents(events)
}
