package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

func TestLoadTokensMigratesLegacyPlaintextToken(t *testing.T) {
	initSettingTestDB(t)
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	userService := &UserService{}

	if err := dbsqlite.DB().Create(&model.Tokens{
		Desc:    "legacy",
		Token:   "legacy-token",
		Enabled: true,
		Expiry:  0,
		UserId:  1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	raw, err := userService.LoadTokens()
	if err != nil {
		t.Fatal(err)
	}
	var loaded []map[string]any
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one loaded token, got %d", len(loaded))
	}
	if loaded[0]["tokenHash"] == "" || loaded[0]["token"] != nil {
		t.Fatalf("loaded token leaked plaintext or missed hash: %#v", loaded[0])
	}

	var stored model.Tokens
	if err := dbsqlite.DB().First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Token != "" {
		t.Fatalf("legacy plaintext token was not cleared: %q", stored.Token)
	}
	if stored.TokenHash == "" || stored.TokenPrefix != tokenPrefix("legacy-token") {
		t.Fatalf("legacy token hash/prefix not populated: %#v", stored)
	}
	if !stored.Enabled || stored.Scope != defaultAPITokenScope {
		t.Fatalf("legacy token defaults not populated: %#v", stored)
	}
}

func TestAddTokenValidatesScopeAllowlist(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}

	for _, scope := range []string{"", "admin", "read", "write", "update", "database", "observability"} {
		if _, err := userService.AddToken("admin", 0, "valid "+scope, scope); err != nil {
			t.Fatalf("scope %q should be accepted: %v", scope, err)
		}
	}
	if _, err := userService.AddToken("admin", 0, "component scope without provider", "fixture-scope"); err == nil {
		t.Fatal("component scope without provider should be rejected")
	}
	unregister := RegisterAPITokenScopeProvider(func() []string { return []string{"component-scope"} })
	if _, err := userService.AddToken("admin", 0, "component scope", "component-scope"); err != nil {
		t.Fatalf("registered component scope should be accepted: %v", err)
	}
	if !apiTokenScopeAllowed("component-scope") {
		t.Fatal("registered component scope should be allowed")
	}
	unregister()
	if apiTokenScopeAllowed("component-scope") {
		t.Fatal("unregistered component scope should stop being allowed")
	}
	if _, err := userService.AddToken("admin", 0, "component scope after unregister", "component-scope"); err == nil {
		t.Fatal("unregistered component scope should be rejected")
	}
	if _, err := userService.AddToken("admin", 0, "invalid", "full"); err == nil {
		t.Fatal("scope full should be rejected")
	}
	if _, err := userService.AddToken("admin", 0, "invalid", "admin "); err != nil {
		t.Fatalf("trimmed admin scope should be accepted: %v", err)
	}

	var tokens []model.Tokens
	if err := dbsqlite.DB().Order("id asc").Find(&tokens).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if token.Scope == "component-scope" {
			continue
		}
		if !apiTokenScopeAllowed(token.Scope) {
			t.Fatalf("stored invalid scope: %#v", token)
		}
	}
}

func TestRecordTokenUseFlushesBatchedUpdate(t *testing.T) {
	initSettingTestDB(t)
	resetTokenUseDebouncerForTest()
	t.Cleanup(resetTokenUseDebouncerForTest)

	userService := &UserService{}
	if err := dbsqlite.DB().Create(&model.Tokens{
		Desc:      "tracked",
		TokenHash: "hash",
		Enabled:   true,
		UserId:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var token model.Tokens
	if err := dbsqlite.DB().Where("desc = ?", "tracked").First(&token).Error; err != nil {
		t.Fatal(err)
	}

	if err := userService.RecordTokenUse(token.Id, "198.51.100.10"); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&token, token.Id).Error; err != nil {
		t.Fatal(err)
	}
	if token.LastUsedAt != 0 || token.LastUsedIP != "" {
		t.Fatalf("token use was written before flush: %#v", token)
	}

	if err := userService.RecordTokenUse(token.Id, "198.51.100.11"); err != nil {
		t.Fatal(err)
	}
	if err := StopTokenUseDebouncer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&token, token.Id).Error; err != nil {
		t.Fatal(err)
	}
	if token.LastUsedAt == 0 || token.LastUsedIP != "198.51.100.11" {
		t.Fatalf("token use was not flushed with latest value: %#v", token)
	}
}
