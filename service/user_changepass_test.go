package service

import (
	"context"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func TestCompletePasswordTransitionDoesNotUpgradeAStaleSQLiteReadTransaction(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	const oldPassword = "Initial administrator secret 2026!"
	const newPassword = "Replacement administrator secret 2026!"
	userService := &UserService{}
	if err := userService.UpdateFirstUser("admin", oldPassword); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", admin.Id).Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}

	db := dbsqlite.DB()
	callbackName := "p18:e02:concurrent-writer-after-user-read"
	writerDone := make(chan error, 1)
	fired := false
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "users" {
			return
		}
		fired = true
		go func() {
			writerDone <- db.Create(&model.Setting{Key: "p18-e02-concurrent-writer", Value: "committed"}).Error
		}()
		if err := <-writerDone; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	result, err := userService.CompletePasswordTransition(context.Background(), admin.Id, oldPassword, "renamed-admin", newPassword)
	if err != nil {
		t.Fatalf("password transition failed after concurrent WAL writer: %v", err)
	}
	if !fired || result.Username != "renamed-admin" {
		t.Fatalf("concurrency fixture or transition result invalid: fired=%v result=%#v", fired, result)
	}
	var stored model.User
	if err := db.Where("id = ?", admin.Id).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ForcePasswordReset {
		t.Fatal("successful transition preserved the forced-reset state")
	}
	if ok, _ := common.CheckPassword(stored.Password, newPassword); !ok {
		t.Fatal("successful transition did not persist the replacement credential")
	}
}

func TestUserServiceChangePassValidatesAndKeepsUsernamesUnique(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}
	if err := userService.UpdateFirstUser("admin", "old-password-value"); err != nil {
		t.Fatal(err)
	}
	if _, err := userService.AddUser("admin", "old-password-value", "bob", "bob-password-value"); err != nil {
		t.Fatal(err)
	}

	for name, change := range map[string]struct {
		oldPass string
		newUser string
		newPass string
	}{
		"empty username":     {oldPass: "old-password-value", newUser: "", newPass: "new-password-value"},
		"blank username":     {oldPass: "old-password-value", newUser: "   ", newPass: "new-password-value"},
		"duplicate username": {oldPass: "old-password-value", newUser: "bob", newPass: "new-password-value"},
		"wrong old password": {oldPass: "wrong", newUser: "admin2", newPass: "new-password-value"},
		"empty password":     {oldPass: "old-password-value", newUser: "admin2", newPass: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := userService.ChangePass("admin", change.oldPass, change.newUser, change.newPass); err == nil {
				t.Fatal("invalid credential change was accepted")
			}
		})
	}

	if err := userService.ChangePass("admin", "old-password-value", " admin2 ", "new-password-value"); err != nil {
		t.Fatal(err)
	}
	var stored model.User
	if err := dbsqlite.DB().Where("username = ?", "admin2").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if ok, _ := common.CheckPassword(stored.Password, "new-password-value"); !ok {
		t.Fatal("new password was not persisted")
	}
	var bobCount int64
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "bob").Count(&bobCount).Error; err != nil {
		t.Fatal(err)
	}
	if bobCount != 1 {
		t.Fatalf("duplicate-name rejection changed the existing user count: %d", bobCount)
	}
}
