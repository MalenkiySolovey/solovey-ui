//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/transportcredentials"
)

func TestMKCPSeedRetainsItsCanonicalOwner(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	svc, _, _ := newHotReloadConfigService(t)
	const seed = "synthetic-owned-mkcp-seed"
	for _, owner := range []string{"inbounds", "outbounds"} {
		transport := map[string]any{"type": "mkcp", "mtu": 1350, "tti": 50, "header_type": "srtp", "seed": map[string]string{"intent": "replace", "value": seed}}
		draft := map[string]any{"type": "vless", "tag": "mkcp-" + owner, "transport": transport}
		if owner == "inbounds" {
			draft["listen"], draft["listen_port"] = "127.0.0.1", 24567
		} else {
			draft["server"], draft["server_port"], draft["uuid"] = "192.0.2.1", 443, "f0aa8764-88a4-4d8e-8ca8-24df4d750382"
		}
		save := func(action string) error {
			raw, _ := json.Marshal(draft)
			_, err := svc.Save(owner, action, raw, "", "admin", "fixture.invalid")
			return err
		}
		if err := save("new"); err != nil {
			t.Fatal("native transport owner save failed", owner, err)
		}
		var stored struct {
			Id               uint
			Options, OutJson json.RawMessage
		}
		if err := db.Table(owner).Where("tag = ?", draft["tag"]).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(stored.Options, []byte(seed)) || bytes.Contains(stored.OutJson, []byte(seed)) || bytes.Contains(stored.OutJson, []byte("sbox:v1:")) {
			t.Fatal("transport seed escaped its canonical encrypted field")
		}
		var view any
		if owner == "inbounds" {
			rows, err := entityinbounds.GetByIDs(db, strconv.Itoa(int(stored.Id)))
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range *rows {
				if row["tag"] == draft["tag"] {
					view = row
				}
			}
		} else {
			rows, err := entityoutbounds.GetAll(db)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range *rows {
				if row["tag"] == draft["tag"] {
					view = row
				}
			}
		}
		encoded, _ := json.Marshal(view)
		if bytes.Contains(encoded, []byte(seed)) || bytes.Contains(encoded, []byte("sbox:v1:")) {
			t.Fatal("normal transport read disclosed private material")
		}
		var masked struct{ Transport struct{ Seed map[string]any } }
		if json.Unmarshal(encoded, &masked) != nil || masked.Transport.Seed["present"] != true {
			t.Fatal("normal transport read lost the SecretRef", owner)
		}
		secret := masked.Transport.Seed
		transport["seed"] = map[string]any{"intent": "unchanged", "reference": secret["reference"], "present": secret["present"], "rotationRevision": secret["rotationRevision"]}
		draft["id"] = stored.Id
		before := append([]byte(nil), stored.Options...)
		if err := save("edit"); err != nil {
			t.Fatal("masked transport edit failed", owner, err)
		}
		if err := db.Table(owner).Where("id = ?", stored.Id).First(&stored).Error; err != nil || !bytes.Equal(before, stored.Options) {
			t.Fatal("masked edit changed the owned seed")
		}
		runtime, err := transportcredentials.Runtime(db, owner, stored.Id, stored.Options)
		if err != nil || !bytes.Contains(runtime, []byte(seed)) {
			t.Fatal("trusted transport projection failed", err)
		}
		if owner == "inbounds" {
			var source model.Inbound
			if err := db.First(&source, stored.Id).Error; err != nil {
				t.Fatal(err)
			}
			beforeTemplate := append([]byte(nil), source.OutJson...)
			delivered, err := entityinbounds.TransportDeliveryCopy(db, &source)
			if err != nil || !bytes.Contains(delivered.OutJson, []byte(seed)) || !bytes.Equal(source.OutJson, beforeTemplate) {
				t.Fatal("transport delivery must borrow from the canonical owner without mutating its template", err)
			}
		}
		transport["seed"] = seed
		if err := save("edit"); err == nil {
			t.Fatal("unowned plaintext seed was accepted")
		}
		transport["seed"] = map[string]string{"intent": "clear"}
		if err := save("edit"); err != nil {
			t.Fatal("explicit seed clear failed", err)
		}
	}
}
