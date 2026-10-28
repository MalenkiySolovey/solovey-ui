//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestNativeOutboundGroupsKeepMemberOwners(t *testing.T) {
	for _, kind := range []string{"bond", "core-failover"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			svc, _, _ := newHotReloadConfigService(t)
			save := func(action string, draft any) error {
				raw, _ := json.Marshal(draft)
				_, err := svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
				return err
			}
			const password = "private-group-member-password-canary"
			leaf := map[string]any{"type": "mieru", "tag": "owned-member", "server": "192.0.2.1", "server_port": 443, "username": "fixture", "password": map[string]string{"intent": "replace", "value": password}, "transport": "TCP"}
			if err := save("new", leaf); err != nil {
				t.Fatal(err)
			}
			group := map[string]any{"type": kind, "tag": "owned-group"}
			if kind == "bond" {
				group["outbounds"] = []any{map[string]any{"outbound": "owned-member", "download_ratio": 100, "upload_ratio": 100, "count": 1}}
			} else {
				group["outbounds"], group["strategy"], group["delay"] = []string{"owned-member"}, "sequential", "25ms"
			}
			if err := save("new", group); err != nil {
				t.Fatal(err)
			}
			var stored model.Outbound
			if err := db.Where("tag = ?", "owned-group").First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(stored.Options, []byte(password)) || bytes.Contains(stored.Options, []byte("sbox:v1:")) || bytes.Contains(stored.Options, []byte(`"type"`)) {
				t.Fatal("native group persisted copied member state or credentials")
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil || bytes.Count(*config, []byte(password)) != 1 {
				t.Fatal("native group duplicated or lost the canonical credential owner", err)
			}
			var document struct {
				Outbounds []map[string]json.RawMessage `json:"outbounds"`
			}
			if json.Unmarshal(*config, &document) != nil {
				t.Fatal("invalid config")
			}
			found := false
			for _, row := range document.Outbounds {
				if string(row["tag"]) == `"owned-group"` {
					found = true
					expected := kind
					if kind == "core-failover" {
						expected = "failover"
					}
					var children []json.RawMessage
					if json.Unmarshal(row["outbounds"], &children) != nil || len(children) != 1 {
						t.Fatal("native group lost its selected member")
					}
					child := children[0]
					if kind == "bond" {
						var leg struct {
							Outbound json.RawMessage `json:"outbound"`
						}
						if json.Unmarshal(child, &leg) != nil {
							t.Fatal("invalid native bond leg")
						}
						child = leg.Outbound
					}
					var header struct {
						Type      string   `json:"type"`
						Outbounds []string `json:"outbounds"`
					}
					if json.Unmarshal(child, &header) != nil || string(row["type"]) != `"`+expected+`"` || header.Type != "selector" || len(header.Outbounds) != 1 || header.Outbounds[0] != "owned-member" {
						t.Fatal("native group runtime identity or member schema changed")
					}
				}
			}
			if !found {
				t.Fatal("native group omitted from Extended projection")
			}
			if err := save("del", "owned-member"); err == nil || !strings.Contains(err.Error(), "referenced") {
				t.Fatal("referenced native member was deleted", err)
			}
			var member model.Outbound
			if err := db.Where("tag = ?", "owned-member").First(&member).Error; err != nil {
				t.Fatal(err)
			}
			leaf["id"] = member.Id
			delete(leaf, "password")
			leaf["server_port"] = 444
			raw, _ := json.Marshal(leaf)
			change, err := entityoutbounds.Save(db, "edit", raw)
			if err != nil || !change.NeedsRestart {
				t.Fatal("member update did not rebuild its captured group", err)
			}
			leaf["detour"] = "owned-group"
			if err := save("edit", leaf); err == nil || !strings.Contains(err.Error(), "OUTBOUND_GROUP_CYCLE") {
				t.Fatal("native group admitted a member detouring back to its parent", err)
			}
			delete(leaf, "detour")
			leaf["tag"] = "renamed-member"
			if err := save("edit", leaf); err == nil {
				t.Fatal("referenced member was renamed")
			}
			group["id"] = stored.Id
			if kind == "bond" {
				group["outbounds"] = []any{map[string]any{"outbound": "owned-group", "download_ratio": 100, "upload_ratio": 100}}
			} else {
				group["outbounds"] = []string{"owned-group"}
			}
			if err := save("edit", group); err == nil || !strings.Contains(err.Error(), "OUTBOUND_GROUP_CYCLE") {
				t.Fatal("native group admitted a recursive owner graph", err)
			}
		})
	}
}
