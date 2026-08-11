package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	defaultAPITokenScope = "admin"
	maxAPITokenScopeLen  = 64
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

func ResetAPITokenScopeProvidersForTest() {
	apiTokenScopeProviders.Lock()
	apiTokenScopeProviders.list = nil
	apiTokenScopeProviders.Unlock()
}

func allowedAPITokenScopes() []string {
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
			scope = normalizeTokenScope(scope)
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

func (s *UserService) LoadTokens() ([]byte, error) {
	if err := s.migrateLegacyTokens(); err != nil {
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
		if t.User == nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"id":          t.Id,
			"tokenHash":   t.TokenHash,
			"tokenPrefix": t.TokenPrefix,
			"scope":       normalizeTokenScope(t.Scope),
			"enabled":     t.Enabled,
			"expiry":      t.Expiry,
			"username":    t.User.Username,
		})
	}
	jsonResult, _ := json.MarshalIndent(result, "", "  ")
	return jsonResult, nil
}

func (s *UserService) GetUserTokens(username string) (*[]model.Tokens, error) {
	if err := s.migrateLegacyTokens(); err != nil {
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
		logger.Warning("get user tokens failed:", err)
		return nil, err
	}
	for i := range token {
		token[i].Token = maskedToken(token[i].TokenPrefix)
		token[i].Scope = normalizeTokenScope(token[i].Scope)
	}
	return &token, nil
}

func (s *UserService) AddToken(username string, expiry int64, desc string, scope string) (string, error) {
	db := dbsqlite.DB()
	scope, err := validateTokenScope(scope)
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
	plainToken := common.Random(32)
	tokenHash, err := s.HashAPIToken(plainToken)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	token := &model.Tokens{
		Desc:        desc,
		TokenHash:   tokenHash,
		TokenPrefix: tokenPrefix(plainToken),
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

func (s *UserService) DeleteToken(id string) error {
	db := dbsqlite.DB()
	return db.Model(model.Tokens{}).Where("id = ?", id).Delete(&model.Tokens{}).Error
}

func (s *UserService) SetTokenEnabled(id string, enabled bool) error {
	return dbsqlite.DB().Model(model.Tokens{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"enabled":    enabled,
			"updated_at": time.Now().Unix(),
		}).Error
}

func (s *UserService) RecordTokenUse(id uint, ip string) error {
	debouncer := s.runtime().tokenUseDebouncer()
	if debouncer != nil {
		debouncer.Record(id, ip, time.Now().Unix())
	}
	return nil
}

func (s *UserService) HashAPIToken(token string) (string, error) {
	salt, err := (&SettingService{}).GetInstallSalt()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write(salt)
	hash.Write([]byte{0})
	hash.Write([]byte(token))
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *UserService) migrateLegacyTokens() error {
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
			"token_prefix": tokenPrefix(token.Token),
			"scope":        normalizeTokenScope(token.Scope),
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

func normalizeTokenScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return defaultAPITokenScope
	}
	return scope
}

func validateTokenScope(scope string) (string, error) {
	scope = normalizeTokenScope(scope)
	if !apiTokenScopeAllowed(scope) {
		return "", common.NewError("invalid token scope")
	}
	return scope, nil
}

func apiTokenScopeAllowed(scope string) bool {
	if len(scope) > maxAPITokenScopeLen {
		return false
	}
	matched := 0
	for _, allowed := range allowedAPITokenScopes() {
		matched |= common.ConstantTimeStringEqual(scope, allowed, maxAPITokenScopeLen)
	}
	return matched == 1
}

func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func maskedToken(prefix string) string {
	if prefix == "" {
		return "****"
	}
	return "****" + prefix
}
