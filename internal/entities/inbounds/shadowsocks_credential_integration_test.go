package entityinbounds_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/sessionidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	"github.com/sagernet/sing-box/option"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestShadowsocksRenderedCredentialsMatchExportsAndIdentity(t *testing.T) {
	baseKey := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	shortKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	credentials, _ := json.Marshal(map[string]any{
		"shadowsocks":   map[string]any{"name": "authenticated32", "password": baseKey},
		"shadowsocks16": map[string]any{"name": "authenticated16", "password": shortKey},
	})
	for _, method := range []string{"aes-128-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"} {
		t.Run(method, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			if err = db.AutoMigrate(&model.Inbound{}, &model.Client{}); err != nil {
				t.Fatal(err)
			}
			key, name := baseKey, "authenticated32"
			if method == "2022-blake3-aes-128-gcm" {
				key, name = shortKey, "authenticated16"
			}
			options, _ := json.Marshal(map[string]any{"method": method, "password": key, "listen": "127.0.0.1", "listen_port": 12345})
			inbound := &model.Inbound{Id: 1, Type: "shadowsocks", Tag: "ss", Options: options, Addrs: json.RawMessage(`[{"server":"127.0.0.1","server_port":12345}]`)}
			if err = entityinbounds.FillOutboundJSON(inbound, "127.0.0.1"); err != nil {
				t.Fatal(err)
			}
			if err = db.Create(inbound).Error; err != nil {
				t.Fatal(err)
			}
			client := model.Client{Id: 7, Name: "unrelated display label", Enable: true, Inbounds: json.RawMessage(`[1]`), Config: credentials}
			if err = db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			for _, peer := range []model.Client{
				{Id: 8, Enable: false, Inbounds: json.RawMessage(`[1]`), Config: credentials},
				{Id: 9, Enable: true, Inbounds: json.RawMessage(`[]`), Config: credentials},
			} {
				if err = db.Create(&peer).Error; err != nil {
					t.Fatal(err)
				}
			}
			raw, err := inbound.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := (entityinbounds.StoredUsers{}).AddUsers(db, raw, inbound.Id, inbound.Type)
			if err != nil {
				t.Fatal(err)
			}
			var native option.ShadowsocksInboundOptions
			if err = json.Unmarshal(rendered, &native); err != nil {
				t.Fatal(err)
			}
			if len(native.Users) != 1 || native.Users[0].Password != key || native.Users[0].Name != name {
				t.Fatal("renderer chose a different authentication credential than the cipher requires")
			}
			bindings, err := sessionidentity.Capture(db, rendered)
			if err != nil || len(bindings) != 1 || bindings[0].ClientID != client.Id || bindings[0].Principal != name {
				t.Fatal("rendered authenticated credential lost its stable member identity")
			}
			outbounds, err := local.BuildInboundOutbounds(client.Config, []*model.Inbound{inbound})
			if err != nil || len(outbounds.Outbounds) != 1 {
				t.Fatal("local export unavailable")
			}
			want := key
			if strings.HasPrefix(method, "2022-") {
				want = key + ":" + key
			}
			if outbounds.Outbounds[0]["password"] != want {
				t.Fatal("local export disagrees with authenticated runtime credential")
			}
			links, err := uri.Generate(client.Config, inbound, "")
			if err != nil || len(links) != 1 {
				t.Fatal("URI export unavailable")
			}
			parsed, _, err := uri.Parse(links[0], 0)
			if err != nil || (*parsed)["password"] != want {
				t.Fatal("URI export disagrees with authenticated runtime credential")
			}
			if method == "2022-blake3-aes-128-gcm" {
				var wrong map[string]any
				if err = json.Unmarshal(rendered, &wrong); err != nil {
					t.Fatal(err)
				}
				wrong["users"] = []map[string]any{{"name": "authenticated32", "password": baseKey}}
				stale, _ := json.Marshal(wrong)
				bindings, err = sessionidentity.Capture(db, stale)
				if err != nil || len(bindings) != 0 {
					t.Fatal("wrong cipher credential associated by its name")
				}
			}
		})
	}
}
