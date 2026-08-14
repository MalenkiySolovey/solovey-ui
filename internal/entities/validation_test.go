package entities

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateStoredRejectsMalformedEntityJSON(t *testing.T) {
	tests := []struct {
		name string
		row  any
	}{
		{"inbound options", &model.Inbound{Type: "mixed", Tag: "in", Options: []byte(`[]`), Addrs: []byte(`[]`), OutJson: []byte(`{}`)}},
		{"inbound address", &model.Inbound{Type: "mixed", Tag: "in-address", Options: []byte(`{}`), Addrs: []byte(`[{}]`), OutJson: []byte(`{}`)}},
		{"outbound options", &model.Outbound{Type: "direct", Tag: "out", Options: []byte(`[]`)}},
		{"endpoint extension", &model.Endpoint{Type: "wireguard", Tag: "endpoint", Options: []byte(`{}`), Ext: []byte(`[]`)}},
		{"service options", &model.Service{Type: "resolved", Tag: "service", Options: []byte(`[]`)}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := fmt.Sprintf("file:stored-entity-json-%d?mode=memory&cache=shared", index)
			db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
			if err != nil {
				if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
					t.Skip(err)
				}
				t.Fatal(err)
			}
			if err := db.AutoMigrate(
				&model.Client{}, &model.Inbound{}, &model.Outbound{}, &model.Endpoint{}, &model.Service{}, &model.Tls{},
			); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(test.row).Error; err != nil {
				t.Fatal(err)
			}
			if err := ValidateStored(db); err == nil {
				t.Fatal("malformed stored JSON was accepted")
			}
		})
	}
}
