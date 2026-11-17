// Package tokens owns Panel API token credentials and durable authorization.
package tokens

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	backendcrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

// InstallSaltProvider supplies the existing settings owner without a Product runtime.
type InstallSaltProvider interface{ InstallSalt() ([]byte, error) }

type Service struct {
	Settings InstallSaltProvider
	Warning  func(...any)
}

func (s *Service) warning(args ...any) {
	if s != nil && s.Warning != nil {
		s.Warning(args...)
	}
}

const (
	DefaultScope         = "admin"
	MaxScopeBytes        = 64
	maxAPITokenProviders = 128
)

var coreAPITokenScopes = []string{
	"admin",
	"read",
	"write",
	"update",
	"database",
	"observability",
}

var ErrTokenNotFound = errors.New("token not found")

// APITokenAuthorization is the current principal and permission of a stored
// token. A cached token is only a candidate; the durable owner decides whether
// it still authorizes this request.
type APITokenAuthorization struct {
	ID          uint
	TokenPrefix string
	Scope       string
	Username    string
}

func (s *Service) AuthorizeAPIToken(ctx context.Context, id uint, hash string, now int64) (APITokenAuthorization, error) {
	var authorization APITokenAuthorization
	db := dbsqlite.DB()
	if db == nil || id == 0 || hash == "" {
		return authorization, ErrTokenNotFound
	}
	err := db.WithContext(ctx).Table("tokens").
		Select("tokens.id, tokens.token_prefix, tokens.scope, users.username").
		Joins("JOIN users ON users.id = tokens.user_id").
		Where("tokens.id = ? AND tokens.token_hash = ? AND tokens.enabled = ? AND (tokens.expiry = 0 OR tokens.expiry > ?) AND users.force_password_reset = ?", id, hash, true, now, false).
		Take(&authorization).Error
	if err != nil {
		return APITokenAuthorization{}, err
	}
	if authorization.Username == "" || !ScopeAllowed(NormalizeScope(authorization.Scope)) {
		return APITokenAuthorization{}, ErrTokenNotFound
	}
	authorization.Scope = NormalizeScope(authorization.Scope)
	return authorization, nil
}

var apiTokenScopeProviders = struct {
	sync.RWMutex
	next uint64
	list map[uint64]func() []string
}{}

func RegisterAPITokenScopeProvider(provider func() []string) func() {
	if provider == nil {
		return func() {}
	}
	apiTokenScopeProviders.Lock()
	if apiTokenScopeProviders.list == nil {
		apiTokenScopeProviders.list = map[uint64]func() []string{}
	}
	if len(apiTokenScopeProviders.list) >= maxAPITokenProviders {
		apiTokenScopeProviders.Unlock()
		panic("API token scope provider registry capacity exceeded")
	}
	id := apiTokenScopeProviders.next
	apiTokenScopeProviders.next++
	apiTokenScopeProviders.list[id] = provider
	apiTokenScopeProviders.Unlock()

	return func() {
		apiTokenScopeProviders.Lock()
		delete(apiTokenScopeProviders.list, id)
		apiTokenScopeProviders.Unlock()
	}
}

func AllowedScopes() []string {
	scopes := append([]string(nil), coreAPITokenScopes...)
	apiTokenScopeProviders.RLock()
	providerIDs := make([]uint64, 0, len(apiTokenScopeProviders.list))
	for id := range apiTokenScopeProviders.list {
		providerIDs = append(providerIDs, id)
	}
	sort.Slice(providerIDs, func(i, j int) bool { return providerIDs[i] < providerIDs[j] })
	providers := make([]func() []string, 0, len(providerIDs))
	for _, id := range providerIDs {
		providers = append(providers, apiTokenScopeProviders.list[id])
	}
	apiTokenScopeProviders.RUnlock()
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		seen[scope] = struct{}{}
	}
	for _, provider := range providers {
		for _, scope := range provider() {
			scope = NormalizeScope(scope)
			if scope == "" {
				continue
			}
			if _, exists := seen[scope]; exists {
				continue
			}
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func (s *Service) LoadTokens() ([]byte, error) {
	if err := s.MigrateLegacyTokens(); err != nil {
		return nil, err
	}
	db := dbsqlite.DB()
	var tokens []model.Tokens
	err := db.Model(model.Tokens{}).Preload("User").
		Where("enabled = ? AND token_hash <> '' AND (expiry = 0 OR expiry > ?)", true, time.Now().Unix()).
		Find(&tokens).Error
	if err != nil {
		return nil, err
	}
	var result []map[string]interface{}
	for _, t := range tokens {
		if t.User == nil || t.User.ForcePasswordReset || t.User.Username == "" {
			continue
		}
		result = append(result, map[string]interface{}{
			"id":          t.Id,
			"tokenHash":   t.TokenHash,
			"tokenPrefix": t.TokenPrefix,
			"scope":       NormalizeScope(t.Scope),
			"enabled":     t.Enabled,
			"expiry":      t.Expiry,
			"username":    t.User.Username,
		})
	}
	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return jsonResult, nil
}

func (s *Service) GetUserTokens(username string) (*[]model.Tokens, error) {
	if err := s.MigrateLegacyTokens(); err != nil {
		return nil, err
	}
	db := dbsqlite.DB()
	var token []model.Tokens
	err := db.Model(model.Tokens{}).
		Select("id, desc, token_prefix, scope, enabled, expiry, user_id, created_at, updated_at, last_used_at, last_used_ip").
		Where("user_id = (select id from users where username = ?)", username).
		Order("id desc").
		Find(&token).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		s.warning("get user tokens failed:", err)
		return nil, err
	}
	for i := range token {
		token[i].Token = MaskedToken(token[i].TokenPrefix)
		token[i].Scope = NormalizeScope(token[i].Scope)
	}
	return &token, nil
}

func (s *Service) AddToken(username string, expiry int64, desc string, scope string) (string, error) {
	db := dbsqlite.DB()
	scope, err := ValidateScope(scope)
	if err != nil {
		return "", err
	}
	var userId uint
	err = db.Model(model.User{}).Where("username = ?", username).Select("id").Scan(&userId).Error
	if err != nil {
		return "", err
	}
	if expiry > 0 {
		expiry = expiry*86400 + time.Now().Unix()
	}
	plainToken, err := backendcrypto.SecureRandom(32)
	if err != nil {
		return "", err
	}
	tokenHash, err := s.HashAPIToken(plainToken)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	token := &model.Tokens{
		Desc:        desc,
		TokenHash:   tokenHash,
		TokenPrefix: TokenPrefix(plainToken),
		Scope:       scope,
		Enabled:     true,
		Expiry:      expiry,
		CreatedAt:   now,
		UpdatedAt:   now,
		UserId:      userId,
	}
	err = db.Create(token).Error
	if err != nil {
		return "", err
	}
	return plainToken, nil
}

func (s *Service) DeleteToken(username, id string) error {
	db := dbsqlite.DB()
	result := db.Where("id = ? AND user_id IN (?)", id,
		db.Model(&model.User{}).Select("id").Where("username = ?", username)).Delete(&model.Tokens{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTokenNotFound
	}
	return nil
}

func (s *Service) SetTokenEnabled(username, id string, enabled bool) error {
	db := dbsqlite.DB()
	result := db.Model(&model.Tokens{}).
		Where("id = ? AND user_id IN (?)", id, db.Model(&model.User{}).Select("id").Where("username = ?", username)).
		Updates(map[string]interface{}{
			"enabled":    enabled,
			"updated_at": time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTokenNotFound
	}
	return nil
}

func (s *Service) HashAPIToken(token string) (string, error) {
	if s == nil || s.Settings == nil {
		return "", errors.New("API token settings are unavailable")
	}
	salt, err := s.Settings.InstallSalt()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write(salt)
	hash.Write([]byte{0})
	hash.Write([]byte(token))
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) MigrateLegacyTokens() error {
	db := dbsqlite.DB()
	var tokens []model.Tokens
	if err := db.Model(model.Tokens{}).Where("(token_hash = '' OR token_hash IS NULL) AND token <> ''").Find(&tokens).Error; err != nil {
		return err
	}
	for _, token := range tokens {
		tokenHash, err := s.HashAPIToken(token.Token)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		updates := map[string]interface{}{
			"token":        "",
			"token_hash":   tokenHash,
			"token_prefix": TokenPrefix(token.Token),
			"scope":        NormalizeScope(token.Scope),
			"updated_at":   now,
		}
		if token.CreatedAt == 0 {
			updates["created_at"] = now
		}
		if err := db.Model(model.Tokens{}).Where("id = ?", token.Id).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func NormalizeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return DefaultScope
	}
	return scope
}

func ValidateScope(scope string) (string, error) {
	scope = NormalizeScope(scope)
	if !ScopeAllowed(scope) {
		return "", errors.New("invalid token scope")
	}
	return scope, nil
}

func ScopeAllowed(scope string) bool {
	if len(scope) > MaxScopeBytes {
		return false
	}
	matched := 0
	for _, allowed := range AllowedScopes() {
		matched |= backendcrypto.ConstantTimeStringEqual(scope, allowed, MaxScopeBytes)
	}
	return matched == 1
}

func TokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func MaskedToken(prefix string) string {
	if prefix == "" {
		return "****"
	}
	return "****" + prefix
}
