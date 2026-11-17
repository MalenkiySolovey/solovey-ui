package service

import (
	"context"
	"time"

	backendmfa "github.com/MalenkiySolovey/solovey-backend/security/mfa"
)

const (
	MFAStatePending               = backendmfa.MFAStatePending
	MFAStatePendingAcknowledgment = backendmfa.MFAStatePendingAcknowledgment
	MFAStateActive                = backendmfa.MFAStateActive
	MFAStateActivePendingRotation = backendmfa.MFAStateActivePendingRotation
	MFAStateActiveRotationAck     = backendmfa.MFAStateActiveRotationAck
	MFAStateActiveRecoveryAck     = backendmfa.MFAStateActiveRecoveryAck

	MFAPendingTTL     = backendmfa.MFAPendingTTL
	RecoveryCodeCount = backendmfa.RecoveryCodeCount
)

type MFAStatus = backendmfa.MFAStatus
type MFAEnrollment = backendmfa.MFAEnrollment

// MFAService preserves the Product-facing API while the MFA state machine and
// durable operations live in Backend security.
type MFAService struct {
	SettingService SettingService
	Now            func() time.Time
}

func (s *MFAService) backend() *backendmfa.Service {
	return &backendmfa.Service{
		Settings: backendSecuritySettings{service: &s.SettingService},
		Now:      s.Now,
	}
}

func (s *MFAService) Status(userID uint) (MFAStatus, error) {
	return s.backend().Status(userID)
}

func (s *MFAService) BeginEnrollment(userID uint, username string) (MFAEnrollment, error) {
	return s.backend().BeginEnrollment(userID, username)
}

func (s *MFAService) ConfirmEnrollment(userID uint, code string) ([]string, error) {
	return s.backend().ConfirmEnrollment(userID, code)
}

func (s *MFAService) AcknowledgeRecoveryCodes(userID uint) error {
	return s.backend().AcknowledgeRecoveryCodes(userID)
}

func (s *MFAService) VerifyTOTP(userID uint, code string) error {
	return s.backend().VerifyTOTP(userID, code)
}

func (s *MFAService) ConsumeRecoveryCode(userID uint, code string) error {
	return s.backend().ConsumeRecoveryCode(userID, code)
}

func (s *MFAService) RotateRecoveryCodes(userID uint) ([]string, error) {
	return s.backend().RotateRecoveryCodes(userID)
}

func (s *MFAService) Disable(userID uint) error {
	return s.backend().Disable(userID)
}

func (s *MFAService) VerifyPassword(ctx context.Context, userID uint, plaintext string) error {
	return s.backend().VerifyPassword(ctx, userID, plaintext)
}

// These helpers retain the package-local compatibility surface used by the
// existing Product tests and user transition path without retaining the MFA
// implementation in Product.
func activeMFAStates() []string {
	return backendmfa.ActiveMFAStates()
}
