//go:build !shtorm_extended

package entityinbounds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

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
			}); err != nil {
				t.Fatal("preservable metadata edit rejected", err)
			}
			assertPreserved := func() {
				t.Helper()
				var actual model.Inbound
				if err := db.First(&actual, 1).Error; err != nil || actual.Tag != "renamed" || !bytes.Equal(actual.Options, stored.Options) || !bytes.Equal(actual.Addrs, stored.Addrs) || !bytes.Equal(actual.OutJson, stored.OutJson) || actual.TlsId != stored.TlsId {
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
				}); err == nil || !strings.Contains(err.Error(), "NATIVE_INBOUND_CAPABILITY_UNAVAILABLE") {
					t.Fatal("Official admitted a change to unavailable protocol state", err)
				}
				assertPreserved()
			}
		})
	}
}
