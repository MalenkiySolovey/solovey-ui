package service

import (
	"time"

	backendstepup "github.com/MalenkiySolovey/solovey-backend/security/stepup"
)

const (
	StepUpTTL = backendstepup.TTL

	stepUpSessionRefMaxBytes = backendstepup.SessionRefMaxBytes
	stepUpRevisionMaxBytes   = backendstepup.RevisionMaxBytes
	stepUpOperationMaxBytes  = backendstepup.OperationMaxBytes
)

type StepUpBinding = backendstepup.Binding
type IssuedStepUpGrant = backendstepup.IssuedGrant

// StepUpService preserves the Product-facing API while Backend security owns
// grant derivation, persistence and one-use consumption.
type StepUpService struct {
	SettingService SettingService
	Now            func() time.Time
}

func (s *StepUpService) backend() *backendstepup.Service {
	return &backendstepup.Service{
		Settings: backendSecuritySettings{service: &s.SettingService},
		Now:      s.Now,
	}
}

func (s *StepUpService) Issue(binding StepUpBinding) (IssuedStepUpGrant, error) {
	return s.backend().Issue(binding)
}

func (s *StepUpService) Consume(token string, binding StepUpBinding) error {
	return s.backend().Consume(token, binding)
}

func (s *StepUpService) DeleteExpired() (int64, error) {
	return s.backend().DeleteExpired()
}

func (s *StepUpService) InvalidateUser(userID uint) error {
	return s.backend().InvalidateUser(userID)
}

func (s *StepUpService) InvalidateSession(sessionRef string) error {
	return s.backend().InvalidateSession(sessionRef)
}

func validateStepUpBinding(binding StepUpBinding) error {
	return backendstepup.ValidateBinding(binding)
}
