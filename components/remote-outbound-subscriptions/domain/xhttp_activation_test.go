//go:build !minimal

package remote

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"gorm.io/gorm"
)

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
