package service

import (
	"strings"
	"testing"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/model"
)

func TestUserServiceLoginHappyWrongAndLastLogin(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}
	if err := userService.UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatal(err)
	}

	username, err := userService.Login("admin", "correct-password", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if username != "admin" {
		t.Fatalf("unexpected login username %q", username)
	}
	if _, err := userService.Login("admin", "wrong-password", "203.0.113.11"); err == nil {
		t.Fatal("wrong password should be rejected")
	}

	var user model.User
	if err := database.GetDB().Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(user.LastLogins, "203.0.113.10") {
		t.Fatalf("last login IP was not recorded: %q", user.LastLogins)
	}
}

func TestUserServiceLoginLockedDocumentedAtAPILayer(t *testing.T) {
	t.Skip("Login lockout is enforced by api checkLoginRateLimit, not UserService.Login; keep service unit boundary unchanged")
}

func TestUserServiceAddTokenScopePersistenceAndInvalidScope(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}

	for _, scope := range []string{"read", "write", "database", "telegram", "observability", "xui_remote"} {
		if _, err := userService.AddToken("admin", 0, "scope "+scope, scope); err != nil {
			t.Fatalf("scope %q should be accepted: %v", scope, err)
		}
	}
	if _, err := userService.AddToken("admin", 0, "bad", "admin:all"); err == nil {
		t.Fatal("invalid scope should be rejected")
	}

	var stored []model.Tokens
	if err := database.GetDB().Order("id asc").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 6 {
		t.Fatalf("expected six stored tokens, got %d", len(stored))
	}
	for _, token := range stored {
		if token.TokenHash == "" || token.TokenPrefix == "" || !token.Enabled {
			t.Fatalf("stored token missing secure fields: %#v", token)
		}
		if !apiTokenScopeAllowed(token.Scope) {
			t.Fatalf("stored invalid scope: %#v", token)
		}
	}
}

func TestUserServiceHashAPITokenDeterministicWithStableInstallSalt(t *testing.T) {
	initSettingTestDB(t)
	settingService := &SettingService{}
	if _, err := settingService.GetInstallSalt(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(model.Setting{}).Where("key = ?", "installSalt").Update("value", "phase2-stable-salt").Error; err != nil {
		t.Fatal(err)
	}

	userService := &UserService{}
	first, err := userService.HashAPIToken("plain-token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := userService.HashAPIToken("plain-token")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash changed with stable installSalt: %q != %q", first, second)
	}

	if err := database.GetDB().Model(model.Setting{}).Where("key = ?", "installSalt").Update("value", "phase2-other-salt").Error; err != nil {
		t.Fatal(err)
	}
	third, err := userService.HashAPIToken("plain-token")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("hash should change when installSalt changes")
	}
}

func TestUserServiceMigrateLegacyTokensKeepsDisabledIssue27(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}
	legacy := model.Tokens{
		Desc:   "disabled legacy",
		Token:  "legacy-disabled-token",
		UserId: 1,
	}
	if err := database.GetDB().Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.Tokens{}).Where("id = ?", legacy.Id).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	var before model.Tokens
	if err := database.GetDB().Where("id = ?", legacy.Id).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before.Enabled {
		t.Fatalf("disabled fixture was not disabled before migration: %#v", before)
	}
	if err := userService.migrateLegacyTokens(); err != nil {
		t.Fatal(err)
	}
	var stored model.Tokens
	if err := database.GetDB().Where("id = ?", legacy.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled {
		t.Fatalf("disabled legacy token was re-enabled: %#v", stored)
	}
	if stored.Token != "" {
		t.Fatalf("legacy plaintext token was not cleared: %q", stored.Token)
	}
	if stored.TokenHash == "" || stored.TokenPrefix == "" {
		t.Fatalf("legacy token hash/prefix not populated: %#v", stored)
	}
	if stored.Scope != defaultAPITokenScope {
		t.Fatalf("legacy token scope not normalized: %#v", stored)
	}
}
