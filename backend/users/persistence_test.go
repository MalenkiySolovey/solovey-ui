package users

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type persistenceProjection struct {
	prepare  func(*gorm.DB, *model.Client, bool) error
	delivery func(*gorm.DB, []*model.Client, string) error
}

func (p persistenceProjection) ProjectClient(_ uint, raw json.RawMessage) (json.RawMessage, error) {
	return raw, nil
}
func (p persistenceProjection) ValidateClientConfig(json.RawMessage) error            { return nil }
func (p persistenceProjection) PrepareSnellCredential(*gorm.DB, *model.Client) error  { return nil }
func (p persistenceProjection) ValidateSnellUserKeys(*gorm.DB, []*model.Client) error { return nil }
func (p persistenceProjection) PrepareManagedCredentials(tx *gorm.DB, c *model.Client, edit bool) error {
	if p.prepare != nil {
		return p.prepare(tx, c, edit)
	}
	return nil
}
func (p persistenceProjection) UpdateLinksWithFixedInbounds(tx *gorm.DB, c []*model.Client, host string) error {
	if p.delivery != nil {
		return p.delivery(tx, c, host)
	}
	return nil
}
func (p persistenceProjection) PreviewWithLocalLinks(*gorm.DB, *[]model.Client, string) error {
	return nil
}

func persistenceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sql.Close() })
	if err := db.AutoMigrate(&model.Client{}, &model.ClientIP{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}
func persistenceData(t *testing.T, c model.Client) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestProtocolDeliveryFailureRollsBackIdentityCredentialAndIPChanges(t *testing.T) {
	db := persistenceDB(t)
	if err := db.Create(&model.ClientIP{ClientName: "new-user", IP: "192.0.2.1"}).Error; err != nil {
		t.Fatal(err)
	}
	deliveryFailure := errors.New("delivery fixture rejected")
	var ownerTx *gorm.DB
	store := Store{Protocols: persistenceProjection{
		prepare: func(tx *gorm.DB, c *model.Client, edit bool) error {
			ownerTx = tx
			if edit {
				t.Fatal("new user was treated as an edit")
			}
			placeholder := *c
			placeholder.Config = json.RawMessage(`{}`)
			placeholder.Links = json.RawMessage(`[]`)
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			c.Id = placeholder.Id
			return tx.Create(&model.Setting{Key: "fixture-credential", Value: "sealed-envelope"}).Error
		},
		delivery: func(tx *gorm.DB, c []*model.Client, _ string) error {
			if tx != ownerTx || len(c) != 1 || c[0].Id == 0 {
				t.Fatal("delivery did not share the reserved identity transaction")
			}
			var count int64
			if err := tx.Model(&model.Setting{}).Where("key = ?", "fixture-credential").Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				t.Fatal("credential mutation was not visible inside the same transaction")
			}
			return deliveryFailure
		},
	}}
	_, err := store.Save(SaveRequest{Tx: db, Action: "new", Data: persistenceData(t, model.Client{Name: "new-user", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)})})
	if !errors.Is(err, deliveryFailure) {
		t.Fatalf("error = %v", err)
	}
	for _, value := range []any{&model.Client{}, &model.Setting{}} {
		var count int64
		if err := db.Model(value).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rolled-back owner state count = %d, error = %v", count, err)
		}
	}
	var history int64
	if err := db.Model(&model.ClientIP{}).Where("client_name = ?", "new-user").Count(&history).Error; err != nil || history != 1 {
		t.Fatalf("IP history was not restored: %d, %v", history, err)
	}
}

func TestCallerRollbackRetainsStoredSecretAndAllOwnerState(t *testing.T) {
	db := persistenceDB(t)
	c := model.Client{Name: "old-name", Enable: true, SubSecret: "retained-secret", SortOrder: 7, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientIP{ClientName: c.Name, IP: "192.0.2.2"}).Error; err != nil {
		t.Fatal(err)
	}
	c.Name = "new-name"
	c.SubSecret = "caller-chosen-secret"
	c.SortOrder = 99
	abort := errors.New("outer owner rollback")
	store := Store{Protocols: persistenceProjection{}}
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := store.Save(SaveRequest{Tx: tx, Action: "edit", Data: persistenceData(t, c)}); err != nil {
			return err
		}
		var staged model.Client
		if err := tx.First(&staged, c.Id).Error; err != nil {
			return err
		}
		if staged.Name != c.Name || staged.SubSecret != "retained-secret" || staged.SortOrder != 7 {
			t.Fatalf("staged edit changed secret/order authority: %#v", staged)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var restored model.Client
	if err := db.First(&restored, c.Id).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Name != "old-name" || restored.SubSecret != "retained-secret" {
		t.Fatal("outer rollback leaked user changes")
	}
	var oldHistory int64
	if err := db.Model(&model.ClientIP{}).Where("client_name = ?", "old-name").Count(&oldHistory).Error; err != nil || oldHistory != 1 {
		t.Fatalf("history rollback: %d, %v", oldHistory, err)
	}
}

func TestSaveRejectsMissingProtocolOwnerBeforeEffects(t *testing.T) {
	db := persistenceDB(t)
	_, err := (Store{}).Save(SaveRequest{Tx: db, Action: "new", Data: json.RawMessage(`{"name":"test","inbounds":[],"links":[]}`)})
	if err == nil || !strings.Contains(err.Error(), "protocol projection is unavailable") {
		t.Fatalf("error = %v", err)
	}
	var count int64
	if err := db.Model(&model.Client{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected save had effects: %d, %v", count, err)
	}
}
