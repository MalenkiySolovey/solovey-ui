//go:build shtorm_extended && with_quic

package entityclients

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func managedClientDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newClientDB(t)
	if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func storedManagedPassword(t *testing.T, client model.Client, kind string) string {
	t.Helper()
	var root map[string]struct{ Name, Password string }
	if json.Unmarshal(client.Config, &root) != nil || !secretbox.IsEncrypted(root[kind].Password) {
		t.Fatal("owner did not persist a sealed credential")
	}
	return root[kind].Password
}

func TestNativeClientSaveInitializesOnceAndRetainsOmittedCredential(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel"} {
		t.Run(kind, func(t *testing.T) {
			db := managedClientDB(t)
			inbound := model.Inbound{Id: 1, Type: kind, Tag: "native", Options: json.RawMessage(`{}`)}
			if err := db.Create(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			client := model.Client{Name: "alice", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`)}
			data, _ := json.Marshal(client)
			if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err != nil {
				t.Fatal(err)
			}
			if err := db.Where("name = ?", "alice").First(&client).Error; err != nil {
				t.Fatal(err)
			}
			captured := storedManagedPassword(t, client, kind)
			projected, err := GetByID(db, strconv.Itoa(int(client.Id)))
			if err != nil || len(*projected) != 1 || bytes.Contains((*projected)[0].Config, []byte(captured)) {
				t.Fatal("normal read failed to project a reference")
			}
			client.Config, client.Desc = json.RawMessage(`{}`), "metadata edit through older editor"
			data, _ = json.Marshal(client)
			if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&client, client.Id).Error; err != nil || storedManagedPassword(t, client, kind) != captured {
				t.Fatal("omitted credential was lost or rotated")
			}
			credential, _ := json.Marshal(map[string]string{"name": client.Name, "password": captured})
			if err := credentials.UseClient(t.Context(), db, client.Id, kind, credential, "save-readback", func(raw []byte) error {
				var user struct{ Name, Password string }
				if json.Unmarshal(raw, &user) != nil || user.Name != "alice" || user.Password == "" || secretbox.IsEncrypted(user.Password) {
					t.Fatal("generated credential cannot be consumed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeInboundAssignmentBackfillsMissingPasswordWithoutRotation(t *testing.T) {
	db := managedClientDB(t)
	client := model.Client{Name: "bob", Enable: true, Config: json.RawMessage(`{"trusttunnel":{"name":"bob"}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-subscription"}
	inbound := model.Inbound{Id: 1, Type: "trusttunnel", Tag: "native", Options: json.RawMessage(`{}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return UpdateClientsOnInboundAdd(tx, strconv.Itoa(int(client.Id)), 1, "") }); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	captured := storedManagedPassword(t, client, "trusttunnel")
	if err := db.Transaction(func(tx *gorm.DB) error {
		rows := []model.Inbound{inbound}
		return UpdateLinksByInboundChange(tx, &rows, "", inbound.Tag)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil || storedManagedPassword(t, client, "trusttunnel") != captured {
		t.Fatal("inbound metadata edit rotated a credential")
	}
}

func TestNativeCredentialSaveRollsBackIdentityKeyAndAuditOnFailedBatch(t *testing.T) {
	db := managedClientDB(t)
	clients := []model.Client{
		{Name: "alice", Config: json.RawMessage(`{"mieru":{"password":{"intent":"replace","value":"synthetic-private-value"}}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)},
		{Name: "bob", Config: json.RawMessage(`{"trusttunnel":{"password":{"intent":"replace","value":""}}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)},
	}
	data, _ := json.Marshal(clients)
	if _, err := Save(SaveRequest{Tx: db, Action: "addbulk", Data: data, SaveBatch: func(tx *gorm.DB, slice any) error { return tx.Save(slice).Error }}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	for _, model := range []any{&model.Client{}, &model.Setting{}, &model.AuditEvent{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed batch left identity, key or mutation audit")
		}
	}
	if err := db.Exec(`CREATE TRIGGER deny_secret_write BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(clients[0])
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err == nil {
		t.Fatal("credential mutation bypassed synchronous audit")
	}
	for _, model := range []any{&model.Client{}, &model.Setting{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("audit rejection left identity or key mutation")
		}
	}
}

func TestNativeCredentialClearAndReassignmentRejectStaleDraft(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel"} {
		t.Run(kind, func(t *testing.T) {
			db := managedClientDB(t)
			if err := db.Create(&model.Inbound{Id: 1, Type: kind, Tag: "native", Options: json.RawMessage(`{}`)}).Error; err != nil {
				t.Fatal(err)
			}
			client := model.Client{Name: "alice", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`)}
			save := func(action string) error {
				data, _ := json.Marshal(client)
				_, err := Save(SaveRequest{Tx: db, Action: action, Data: data})
				return err
			}
			if err := save("new"); err != nil {
				t.Fatal(err)
			}
			if err := db.Where("name = ?", "alice").First(&client).Error; err != nil {
				t.Fatal(err)
			}
			original := storedManagedPassword(t, client, kind)
			setMutation := func(mutation any) {
				credential, _ := json.Marshal(map[string]any{"name": "alice", "password": mutation})
				client.Config, _ = json.Marshal(map[string]json.RawMessage{kind: credential})
			}
			setMutation(map[string]any{"intent": "clear"})
			if err := save("edit"); err == nil {
				t.Fatal("assigned native credential was cleared")
			}
			var stored model.Client
			if err := db.First(&stored, client.Id).Error; err != nil || storedManagedPassword(t, stored, kind) != original {
				t.Fatal("rejected clear changed the stored credential")
			}
			client.Inbounds = json.RawMessage(`[]`)
			if err := save("edit"); err != nil {
				t.Fatal("unassigned credential could not be cleared", err)
			}
			projected, err := GetByID(db, strconv.Itoa(int(client.Id)))
			if err != nil {
				t.Fatal(err)
			}
			var root map[string]struct{ Password secretref.View }
			if json.Unmarshal((*projected)[0].Config, &root) != nil || root[kind].Password.Present {
				t.Fatal("cleared value did not project an absent reference")
			}
			view := root[kind].Password
			keep := map[string]any{"intent": "unchanged", "reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision}
			setMutation(keep)
			client.Inbounds = json.RawMessage(`[1]`)
			if err := save("edit"); err != nil {
				t.Fatal("reassignment did not initialize an absent credential", err)
			}
			if err := db.First(&stored, client.Id).Error; err != nil || storedManagedPassword(t, stored, kind) == original {
				t.Fatal("reassignment reused a cleared credential")
			}
			rotated := append([]byte(nil), stored.Config...)
			if err := save("edit"); err == nil {
				t.Fatal("stale absent-reference draft overwrote a new credential")
			}
			if err := db.First(&stored, client.Id).Error; err != nil || !bytes.Equal(stored.Config, rotated) {
				t.Fatal("stale draft rejection changed the credential")
			}
		})
	}
}
