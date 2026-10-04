package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestTokenMutationsRequireAuthenticatedOwner(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	other := model.User{Username: "token-owner-other"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"delete", "enable", "disable"} {
		t.Run(mutation, func(t *testing.T) {
			owned := model.Tokens{UserId: 1, Desc: "owned", TokenHash: "owned-fixture-digest", Scope: "read"}
			foreign := model.Tokens{UserId: other.Id, Desc: "foreign", TokenHash: "foreign-fixture-digest", Scope: "read"}
			for _, token := range []*model.Tokens{&owned, &foreign} {
				if err := db.Create(token).Error; err != nil {
					t.Fatal(err)
				}
			}
			mutate := func(actor, id string) error {
				s := &UserService{}
				if mutation == "delete" {
					return s.DeleteToken(actor, id)
				}
				return s.SetTokenEnabled(actor, id, mutation == "enable")
			}
			for _, denied := range []struct{ actor, id string }{
				{"admin", strconv.FormatUint(uint64(foreign.Id), 10)},
				{"admin", "999999999"},
				{"", strconv.FormatUint(uint64(owned.Id), 10)},
				{"admin", ""},
			} {
				if err := mutate(denied.actor, denied.id); !errors.Is(err, ErrTokenNotFound) {
					t.Fatalf("denied %s must return the same not-found error: %v", mutation, err)
				}
			}
			var retained model.Tokens
			if err := db.First(&retained, foreign.Id).Error; err != nil {
				t.Fatal(err)
			}
			if !retained.Enabled || retained.UpdatedAt != foreign.UpdatedAt {
				t.Fatal("foreign token changed")
			}
			if err := mutate("admin", strconv.FormatUint(uint64(owned.Id), 10)); err != nil {
				t.Fatalf("own token mutation failed: %v", err)
			}
			if mutation == "delete" {
				if err := mutate("admin", strconv.FormatUint(uint64(owned.Id), 10)); !errors.Is(err, ErrTokenNotFound) {
					t.Fatalf("deleted token must not report another success: %v", err)
				}
			} else {
				var changed model.Tokens
				if err := db.First(&changed, owned.Id).Error; err != nil {
					t.Fatal(err)
				}
				if changed.Enabled != (mutation == "enable") {
					t.Fatal("own token did not change")
				}
			}
		})
	}
}

func TestLoadTokensAndAuthorizationUseCurrentOwnerPolicy(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	token := model.Tokens{UserId: 1, TokenHash: "policy-fixture-digest", Scope: "read"}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	s := &UserService{}
	authorize := func() (APITokenAuthorization, error) {
		return s.AuthorizeAPIToken(context.Background(), token.Id, token.TokenHash, time.Now().Unix())
	}
	if principal, err := authorize(); err != nil || principal.Username != "admin" || principal.Scope != "read" {
		t.Fatalf("usable owner denied: err=%v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"username": "current-owner", "force_password_reset": true}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := authorize(); err == nil {
		t.Fatal("reset policy must deny an already cached token")
	}
	raw, err := s.LoadTokens()
	if err != nil {
		t.Fatal(err)
	}
	var loaded []struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatal("reset owner was loaded for authorization")
	}
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	if principal, err := authorize(); err != nil || principal.Username != "current-owner" {
		t.Fatalf("completed transition did not resolve current identity: err=%v", err)
	}
	if err := s.SetTokenEnabled("current-owner", strconv.FormatUint(uint64(token.Id), 10), false); err != nil {
		t.Fatal(err)
	}
	if _, err := authorize(); err == nil {
		t.Fatal("disabled token authorized")
	}
	if _, err := s.AuthorizeAPIToken(context.Background(), token.Id, "different-fixture-digest", time.Now().Unix()); err == nil {
		t.Fatal("mismatched digest authorized")
	}
}
