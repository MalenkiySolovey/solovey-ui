package service

import (
	"time"

	backendsecuritysessions "github.com/MalenkiySolovey/solovey-backend/security/sessions"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-backend/realtime"
)

const (
	DefaultSessionPageSize  = backendsecuritysessions.DefaultSessionPageSize
	MaxSessionPageSize      = backendsecuritysessions.MaxSessionPageSize
	MaxSessionCleanupBatch  = backendsecuritysessions.MaxSessionCleanupBatch
	sessionHistoryRetention = 30 * 24 * time.Hour
)

type SessionInventoryItem = backendsecuritysessions.SessionInventoryItem
type SessionInventory = backendsecuritysessions.SessionInventory

// SecuritySessionService preserves the Product-facing service shape while the
// session state machine and database operations live in Backend security.
type SecuritySessionService struct {
	Now          func() time.Time
	CloseSession func(ref, reason string)
}

func (s *SecuritySessionService) backend() *backendsecuritysessions.SecuritySessionService {
	var now func() time.Time
	var closeSession func(string, string)
	if s != nil {
		now = s.Now
		closeSession = s.CloseSession
	}
	if closeSession == nil {
		closeSession = realtime.CloseSession
	}
	return &backendsecuritysessions.SecuritySessionService{Now: now, CloseSession: closeSession}
}

func (s *SecuritySessionService) List(userID uint, currentRef, cursor string, limit int) (SessionInventory, error) {
	return s.backend().List(userID, currentRef, cursor, limit)
}

func (s *SecuritySessionService) CleanupExpired(limit int) (int64, error) {
	return s.backend().CleanupExpired(limit)
}

func (s *SecuritySessionService) Revoke(userID uint, ref, reason string) error {
	return s.backend().Revoke(userID, ref, reason)
}

func (s *SecuritySessionService) RevokeOthers(userID uint, currentRef, reason string) (int64, error) {
	return s.backend().RevokeOthers(userID, currentRef, reason)
}

func (s *SecuritySessionService) Validate(ref string, userID uint, credentialGeneration, mfaGeneration uint64) (model.SecuritySession, error) {
	return s.backend().Validate(ref, userID, credentialGeneration, mfaGeneration)
}
