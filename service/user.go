package service

import (
	"context"

	backendaccounts "github.com/MalenkiySolovey/solovey-backend/security/accounts"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

// UserService composes the existing Product integrations for the canonical
// Backend administrator and API token owners.
type UserService struct{ Runtime *Runtime }

type DeleteUserResult = backendaccounts.DeleteUserResult
type PasswordTransitionResult = backendaccounts.PasswordTransitionResult

func (s *UserService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

func (s *UserService) accounts() *backendaccounts.Service {
	return &backendaccounts.Service{
		NextSortOrder:           func(tx *gorm.DB) (int, error) { return entityorder.Next(tx, &model.User{}) },
		ValidateImportName:      entityidentity.ValidateName,
		Warning:                 logger.Warning,
		RecordAudit:             (&AuditService{}).Record,
		RemoveInitialCredential: dbsqlite.RemoveInitialAdminPasswordFile,
	}
}

func (s *UserService) GetFirstUser() (*model.User, error) { return s.accounts().GetFirstUser() }
func (s *UserService) UpdateFirstUser(username, password string) error {
	return s.accounts().UpdateFirstUser(username, password)
}
func (s *UserService) Authenticate(ctx context.Context, username, plaintext, remoteIP string) (AuthenticationResult, error) {
	return s.accounts().Authenticate(ctx, username, plaintext, remoteIP)
}
func (s *UserService) CheckUser(username, password, remoteIP string) (*model.User, bool) {
	return s.accounts().CheckUser(username, password, remoteIP)
}
func (s *UserService) RecordLogin(username, remoteIP string) {
	s.accounts().RecordLogin(username, remoteIP)
}
func (s *UserService) GetUsers() (*[]model.User, error) { return s.accounts().GetUsers() }
func (s *UserService) AddUser(actorUsername, currentPass, newUsername, newPassword string) (*model.User, error) {
	return s.accounts().AddUser(actorUsername, currentPass, newUsername, newPassword)
}
func (s *UserService) DeleteUser(actorUsername, currentPass, targetID string) (DeleteUserResult, error) {
	return s.accounts().DeleteUser(actorUsername, currentPass, targetID)
}
func (s *UserService) ChangePass(username, oldPass, newUser, newPass string) error {
	return s.accounts().ChangePass(username, oldPass, newUser, newPass)
}
func (s *UserService) CompletePasswordTransition(ctx context.Context, userID uint, oldPass, newUsername, newPass string) (PasswordTransitionResult, error) {
	return s.accounts().CompletePasswordTransition(ctx, userID, oldPass, newUsername, newPass)
}
func (s *UserService) ChangeCredentialAfterStepUp(ctx context.Context, userID uint, newUsername, newPass string) (PasswordTransitionResult, error) {
	return s.accounts().ChangeCredentialAfterStepUp(ctx, userID, newUsername, newPass)
}
func (s *UserService) CompleteRecoveryTransition(ctx context.Context, userID uint, newUsername, newPass string) (PasswordTransitionResult, error) {
	return s.accounts().CompleteRecoveryTransition(ctx, userID, newUsername, newPass)
}

func nonzeroGeneration(value uint64) uint64 { return backendaccounts.NormalizeGeneration(value) }
