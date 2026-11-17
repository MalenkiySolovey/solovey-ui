package service

import (
	"context"
	"time"

	backendtokens "github.com/MalenkiySolovey/solovey-backend/security/tokens"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	defaultAPITokenScope = backendtokens.DefaultScope
	maxAPITokenScopeLen  = backendtokens.MaxScopeBytes
)

var ErrTokenNotFound = backendtokens.ErrTokenNotFound

type APITokenAuthorization = backendtokens.APITokenAuthorization

func (s *UserService) tokens() *backendtokens.Service {
	return &backendtokens.Service{Settings: backendSecuritySettings{service: &SettingService{}}, Warning: logger.Warning}
}

func RegisterAPITokenScopeProvider(provider func() []string) func() {
	return backendtokens.RegisterAPITokenScopeProvider(provider)
}
func (s *UserService) AuthorizeAPIToken(ctx context.Context, id uint, hash string, now int64) (APITokenAuthorization, error) {
	return s.tokens().AuthorizeAPIToken(ctx, id, hash, now)
}
func (s *UserService) LoadTokens() ([]byte, error) { return s.tokens().LoadTokens() }
func (s *UserService) GetUserTokens(username string) (*[]model.Tokens, error) {
	return s.tokens().GetUserTokens(username)
}
func (s *UserService) AddToken(username string, expiry int64, desc, scope string) (string, error) {
	return s.tokens().AddToken(username, expiry, desc, scope)
}
func (s *UserService) DeleteToken(username, id string) error {
	return s.tokens().DeleteToken(username, id)
}
func (s *UserService) SetTokenEnabled(username, id string, enabled bool) error {
	return s.tokens().SetTokenEnabled(username, id, enabled)
}
func (s *UserService) RecordTokenUse(id uint, ip string) error {
	debouncer := s.runtime().tokenUseDebouncer()
	if debouncer != nil {
		debouncer.Record(id, ip, time.Now().Unix())
	}
	return nil
}

func (s *UserService) HashAPIToken(token string) (string, error) {
	return s.tokens().HashAPIToken(token)
}
func (s *UserService) migrateLegacyTokens() error { return s.tokens().MigrateLegacyTokens() }

func allowedAPITokenScopes() []string        { return backendtokens.AllowedScopes() }
func apiTokenScopeAllowed(scope string) bool { return backendtokens.ScopeAllowed(scope) }
func tokenPrefix(token string) string        { return backendtokens.TokenPrefix(token) }
