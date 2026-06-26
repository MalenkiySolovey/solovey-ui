package service

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func TestUserServiceChangePassValidatesAndKeepsUsernamesUnique(t *testing.T) {
	initSettingTestDB(t)
	userService := &UserService{}
	if err := userService.UpdateFirstUser("admin", "old-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := userService.AddUser("admin", "old-password", "bob", "bob-password"); err != nil {
		t.Fatal(err)
	}

	for name, change := range map[string]struct {
		oldPass string
		newUser string
		newPass string
	}{
		"empty username":     {oldPass: "old-password", newUser: "", newPass: "new-password"},
		"blank username":     {oldPass: "old-password", newUser: "   ", newPass: "new-password"},
		"duplicate username": {oldPass: "old-password", newUser: "bob", newPass: "new-password"},
		"wrong old password": {oldPass: "wrong", newUser: "admin2", newPass: "new-password"},
		"empty password":     {oldPass: "old-password", newUser: "admin2", newPass: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := userService.ChangePass("admin", change.oldPass, change.newUser, change.newPass); err == nil {
				t.Fatal("invalid credential change was accepted")
			}
		})
	}

	if err := userService.ChangePass("admin", "old-password", " admin2 ", "new-password"); err != nil {
		t.Fatal(err)
	}
	var stored model.User
	if err := dbsqlite.DB().Where("username = ?", "admin2").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if ok, _ := common.CheckPassword(stored.Password, "new-password"); !ok {
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
