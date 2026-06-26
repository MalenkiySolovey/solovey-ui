package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPaidDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}, &model.Changes{}, &paid.Binding{}, &paid.Tariff{}, &paid.PaymentOrder{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSetBindingKeepsTelegramAndClientOneToOne(t *testing.T) {
	db := newPaidDB(t)
	clients := []model.Client{
		{Name: "alice", Inbounds: json.RawMessage("[]")},
		{Name: "bob", Inbounds: json.RawMessage("[]")},
	}
	if err := db.Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetBinding(db, clients[0].Id, 77, 100); err != nil {
		t.Fatal(err)
	}
	if err := SetBinding(db, clients[1].Id, 77, 200); err != nil {
		t.Fatal(err)
	}

	var bindings []paid.Binding
	if err := db.Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].ClientId != clients[1].Id || bindings[0].TgUserId != 77 {
		t.Fatalf("bindings = %#v", bindings)
	}

	client, err := ClientByTelegramUserID(db, 77)
	if err != nil {
		t.Fatal(err)
	}
	if client.Id != clients[1].Id {
		t.Fatalf("client id = %d, want %d", client.Id, clients[1].Id)
	}
}
