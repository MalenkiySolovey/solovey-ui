//go:build !shtorm_extended

package entityinbounds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"gorm.io/gorm"
)

func TestOfficialHysteriaRealmInboundUsesSelectedCapabilityAndCredentialOwner(t *testing.T) {
	db := newInboundDB(t)
	if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	ref, _ := ownerdb.Reference("inbounds", 1, "realm.token")
	sealed, err := ownerdb.Apply(db, ref, "", json.RawMessage(`{"intent":"replace","value":"synthetic-official-realm-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]any{"listen_port": 23456, "realm": map[string]any{"server_url": "https://realm.example", "realm_id": "peer", "token": sealed}})
	stored := model.Inbound{Id: 1, Type: "hysteria2", Tag: "realm-history", Options: options, Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{ "type":"hysteria2","realm":{"realm_id":"peer"} }`)}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	view, err := GetByIDs(db, "1")
	masked, _ := json.Marshal(view)
	if err != nil || len(*view) != 1 || bytes.Contains(masked, []byte(sealed)) || bytes.Contains(masked, []byte("synthetic-official-realm-token")) || !bytes.Contains(masked, []byte(`"present":true`)) {
		t.Fatal("historical Realm resource was hidden or disclosed its captured token", err)
	}
	available := entitycapabilities.ResolveResource("inbounds", stored.Type, stored.Options).Available
	runtime, err := GetAllConfig(db, nil)
	if err != nil || available && (len(runtime) != 1 || !bytes.Contains(runtime[0], []byte("synthetic-official-realm-token"))) || !available && len(runtime) != 0 {
		t.Fatal("Realm projection disagreed with the selected SDK capability", err)
	}
	if !available {
		for _, action := range []string{"new", "edit"} {
			draft := json.RawMessage(`{"id":1,"type":"hysteria2","tag":"realm-history","listen_port":23456,"realm":{"server_url":"https://realm.example","realm_id":"peer"}}`)
			if _, err := Save(SaveRequest{Tx: db, Action: action, Data: draft}); err == nil {
				t.Fatal("Official admitted an unavailable Realm resource")
			}
		}
	}
	var after model.Inbound
	if err := db.First(&after, stored.Id).Error; err != nil || !bytes.Equal(after.Options, stored.Options) || !bytes.Equal(after.OutJson, stored.OutJson) || after.Id != stored.Id {
		t.Fatal("Official changed preserved Realm desired state", err)
	}
}

func TestOfficialNativeInboundPreservesUnsupportedFieldsAndBlocksChanges(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel", "ssh", "mtproxy"} {
		t.Run(kind, func(t *testing.T) {
			db := newInboundDB(t)
			payload := func(id uint, tag, transport string, counter uint64) json.RawMessage {
				return json.RawMessage(fmt.Sprintf(`{"id":%d,"type":%q,"tag":%q,"transport":%q,"listen_port":23456,"future_option":{"counter":%d},"addrs":[],"out_json":{"type":%q,"future":true}}`, id, kind, tag, transport, counter, kind))
			}
			if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: payload(0, "native", "TCP", 9007199254740993)}); err == nil {
				t.Fatal("Official admitted a new unavailable native inbound")
			}
			stored := model.Inbound{Id: 1, Type: kind, Tag: "native", Options: json.RawMessage("{ \"transport\": \"TCP\", \"listen_port\":23456, \"future_option\":{\"counter\":9007199254740993} }"), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{ "type":"` + kind + `", "future":true }`)}
			if err := db.Create(&stored).Error; err != nil {
				t.Fatal(err)
			}
			edit := payload(1, "renamed", "TCP", 9007199254740993)
			if err := db.Transaction(func(tx *gorm.DB) error {
				_, err := Save(SaveRequest{Tx: tx, Action: "edit", Data: edit})
				return err
			}); err == nil {
				t.Fatal("unavailable history admitted to the ordinary editor")
			}
			assertPreserved := func() {
				t.Helper()
				var actual model.Inbound
				if err := db.First(&actual, 1).Error; err != nil || actual.Tag != stored.Tag || !bytes.Equal(actual.Options, stored.Options) || !bytes.Equal(actual.Addrs, stored.Addrs) || !bytes.Equal(actual.OutJson, stored.OutJson) || actual.TlsId != stored.TlsId {
					t.Fatal("unavailable owner state was changed or dropped")
				}
			}
			assertPreserved()
			for _, criticalEdit := range []json.RawMessage{
				payload(1, "renamed", "UDP", 9007199254740993),
				payload(1, "renamed", "TCP", 9007199254740992),
				json.RawMessage(strings.Replace(string(edit), `"future":true`, `"future":false`, 1)),
				json.RawMessage(strings.Replace(string(edit), `"listen_port":23456`, `"listen_port":23457`, 1)),
			} {
				if err := db.Transaction(func(tx *gorm.DB) error {
					_, err := Save(SaveRequest{Tx: tx, Action: "edit", Data: criticalEdit})
					return err
				}); err == nil {
					t.Fatal("Official admitted a change to unavailable protocol state", err)
				}
				assertPreserved()
			}
		})
	}
}
