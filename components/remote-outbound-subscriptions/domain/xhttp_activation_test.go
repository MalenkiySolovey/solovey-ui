//go:build !minimal

package remote

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"gorm.io/gorm"
)

func TestFetchedRealmActivationUsesCanonicalCredentialOwner(t *testing.T) {
	db := newRemoteSyncDB(t)
	if err := db.AutoMigrate(&model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	sub := RemoteOutboundSubscription{Name: "Realm", TagPrefix: "realm-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	const token = "private-imported-realm-token"
	fetched, err := ParseFetchedSubscription(`{"outbounds":[{"type":"hysteria2","tag":"peer","realm":{"server_url":"https://realm.invalid","realm_id":"peer","token":"` + token + `","stun_servers":["127.0.0.1:3478"]},"password":"protocol-auth","tls":{"enabled":true}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RefreshFetchedSubscription(db, &sub, fetched, 100); err != nil {
		t.Fatal(err)
	}
	var connection RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	captured := append([]byte(nil), connection.Options...)
	err = db.Transaction(func(tx *gorm.DB) error { return SyncConnectionToOutbound(tx, &connection, true) })
	var rows []model.Outbound
	if query := db.Find(&rows).Error; query != nil {
		t.Fatal(query)
	}
	if !entitycapabilities.ResolveResource("outbounds", connection.Type, captured).Available {
		if err == nil || len(rows) != 0 {
			t.Fatal("unavailable Realm acquired runtime state")
		}
	} else {
		if err != nil || len(rows) != 1 || !connection.Synced || connection.OutboundId == nil || bytes.Contains(rows[0].Options, []byte(token)) {
			t.Fatal("Realm activation bypassed credential ownership", err)
		}
		view, err := entityoutbounds.GetAll(db)
		masked, _ := json.Marshal(view)
		if err != nil || bytes.Contains(masked, []byte(token)) || bytes.Contains(masked, []byte("sbox:v1:")) || !bytes.Contains(masked, []byte(`"present":true`)) {
			t.Fatal("normal Realm read exposed credentials", err)
		}
		config, err := entityoutbounds.GetAllConfig(db)
		runtime, _ := json.Marshal(config)
		if err != nil || !bytes.Contains(runtime, []byte(token)) || bytes.Contains(runtime, []byte("sbox:v1:")) {
			t.Fatal("runtime did not borrow the owned Realm token", err)
		}
	}
	var preserved RemoteOutboundConnection
	if err := db.First(&preserved, connection.Id).Error; err != nil || !bytes.Equal(preserved.Options, captured) {
		t.Fatal("activation rewrote the captured subscription", err)
	}
}

func TestFetchedXHTTPActivationUsesCurrentProviderOwner(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := RemoteOutboundSubscription{Name: "Native", TagPrefix: "native-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	fetched, err := ParseFetchedSubscription("vless://00000000-0000-4000-8000-000000000001@edge.example:443?security=tls&type=xhttp&mode=stream-up&host=cdn.example&path=%2Fliteral%3Fed%3D2048#native")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RefreshFetchedSubscription(db, &sub, fetched, 100); err != nil {
		t.Fatal(err)
	}
	var connection RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error { return SyncConnectionToOutbound(tx, &connection, true) })
	var rows []model.Outbound
	if query := db.Find(&rows).Error; query != nil {
		t.Fatal(query)
	}
	if !entitycapabilities.ResolveResource("outbounds", connection.Type, connection.Options).Available {
		if err == nil || len(rows) != 0 {
			t.Fatal("unavailable transport acquired runtime state")
		}
		return
	}
	if err != nil || len(rows) != 1 {
		t.Fatal("available native transport could not activate through its owner", err)
	}
	var options struct {
		Transport map[string]any `json:"transport"`
	}
	if json.Unmarshal(rows[0].Options, &options) != nil || options.Transport["type"] != "xhttp" || options.Transport["mode"] != "stream-up" || options.Transport["path"] != "/literal?ed=2048" {
		t.Fatal("activation changed native transport semantics")
	}
}
