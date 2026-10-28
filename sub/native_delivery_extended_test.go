//go:build shtorm_extended && with_quic

package sub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
)

func TestNativeSudokuDeliveryUsesAssignedSplitKey(t *testing.T) {
	initSubTestDB(t)
	db := dbsqlite.DB()
	master, err := credentials.GenerateSudokuMaster()
	if err != nil {
		t.Fatal(err)
	}
	public, err := credentials.SudokuPublicKey(master)
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := ownerdb.Reference("inbounds", 1, "master_key")
	mutation, _ := json.Marshal(map[string]string{"intent": "replace", "value": master})
	sealed, err := ownerdb.Apply(db, ref, "", mutation)
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]any{"key": public, "master_key": sealed, "listen_port": 3443, "table_type": "prefer_ascii", "enable_pure_downlink": false, "http_mask_mode": "ws", "path_root": "/public"})
	inbound := model.Inbound{Id: 1, Type: "sudoku", Tag: "Sudoku public", Options: options, Addrs: json.RawMessage(`[{"server":"edge.example","server_port":3443}]`), OutJson: json.RawMessage(`{}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "private-sudoku", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`)}
	raw, _ := json.Marshal(client)
	if _, err := entityclients.Save(entityclients.SaveRequest{Tx: db, Action: "new", Data: raw}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(client.Config)
	linksBefore := bytes.Clone(client.Links)
	body, _, err := (&JSONService{}).GetJSON(client.SubSecret)
	if err != nil || body == nil {
		t.Fatal("private Sudoku JSON unavailable", err)
	}
	var profile struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if json.Unmarshal([]byte(*body), &profile) != nil {
		t.Fatal("invalid private JSON")
	}
	var endpoint canonical.EndpointIR
	found := false
	for _, outbound := range profile.Outbounds {
		if outbound["type"] != "sudoku" {
			continue
		}
		found = true
		key, _ := outbound["key"].(string)
		if key == master || key == public || !credentials.SudokuSplitMatches(key, master) {
			t.Fatal("delivery did not use the assigned split key")
		}
		if _, leaked := outbound["master_key"]; leaked {
			t.Fatal("serving master entered client profile")
		}
		endpoint = canonical.ObserveOutbound(canonical.FormatSingBox, outbound, 0)
	}
	if !found {
		t.Fatal("native Sudoku endpoint omitted")
	}
	links, _, err := (&SubService{}).GetSubs(client.SubSecret)
	if err != nil || links == nil {
		t.Fatal("private Sudoku URI unavailable", err)
	}
	text := *links
	if !strings.Contains(text, "://") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		text = string(decoded)
	}
	imported, _, err := suburi.Parse(strings.TrimSpace(text), 0)
	if err != nil || canonical.ObserveOutbound(canonical.FormatURI, *imported, 0).EndpointID != endpoint.EndpointID {
		t.Fatal("private JSON and compact link disagree", err)
	}
	if err := db.First(&client, client.Id).Error; err != nil || !bytes.Equal(before, client.Config) || !bytes.Equal(linksBefore, client.Links) {
		t.Fatal("delivery changed canonical User state")
	}
	var stored model.Inbound
	if err := db.First(&stored, inbound.Id).Error; err != nil || !bytes.Equal(options, stored.Options) || !bytes.Equal(inbound.OutJson, stored.OutJson) {
		t.Fatal("delivery changed serving ownership")
	}
}

func TestNativePrivateLinksAreResponseOnly(t *testing.T) {
	initSubTestDB(t)
	db := dbsqlite.DB()
	rows := []model.Inbound{
		{Id: 1, Type: "mieru", Tag: "Mieru public", Options: json.RawMessage(`{"listen_port":2443,"transport":"TCP","listen_ports":["3443:3445"],"mtu":1400}`), Addrs: json.RawMessage(`[{"server":"edge.example","server_port":2443}]`), OutJson: json.RawMessage(`{}`)},
		{Id: 2, Type: "mtproxy", Tag: "Telegram", Options: json.RawMessage(`{"listen_port":2444}`), Addrs: json.RawMessage(`[{"server":"edge.example","server_port":2444}]`), OutJson: json.RawMessage(`{}`)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := credentials.Generate("mtproxy")
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"mieru": map[string]any{"password": map[string]string{"intent": "replace", "value": "synthetic-delivery"}}, "mtproxy": map[string]any{"secret": map[string]string{"intent": "replace", "value": secret}}})
	client := model.Client{Name: "private-native", Enable: true, Config: config, Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`)}
	raw, _ := json.Marshal(client)
	if _, err := entityclients.Save(entityclients.SaveRequest{Tx: db, Action: "new", Data: raw}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	configBefore, linksBefore := bytes.Clone(client.Config), bytes.Clone(client.Links)
	if bytes.Contains(linksBefore, []byte("synthetic-delivery")) || bytes.Contains(linksBefore, []byte(secret)) {
		t.Fatal("private link was persisted")
	}
	body, _, err := (&SubService{}).GetSubs(client.SubSecret)
	if err != nil || body == nil {
		t.Fatal("private native links unavailable", err)
	}
	text := *body
	if !strings.Contains(text, "://") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			t.Fatal("invalid encoded subscription")
		}
		text = string(decoded)
	}
	found := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		u, err := url.Parse(line)
		if err != nil {
			t.Fatal("invalid private share link")
		}
		switch u.Scheme {
		case "mierus":
			password, _ := u.User.Password()
			if password != "synthetic-delivery" || u.User.Username() != client.Name || len(u.Query()["port"]) != 2 || u.Query().Get("mtu") != "1400" {
				t.Fatal("Mieru link changed owner facts")
			}
			found["mieru"] = true
		case "tg":
			if u.Query().Get("secret") != secret || u.Query().Get("server") != "edge.example" {
				t.Fatal("Telegram link changed owner facts")
			}
			found["mtproxy"] = true
		}
	}
	if len(found) != 2 {
		t.Fatal("native share link omitted")
	}
	if err := db.First(&client, client.Id).Error; err != nil || !bytes.Equal(configBefore, client.Config) || !bytes.Equal(linksBefore, client.Links) {
		t.Fatal("private delivery changed canonical state")
	}
}

func TestNativePrivateJSONUsesAssignedOwnerCredentials(t *testing.T) {
	initSubTestDB(t)
	db := dbsqlite.DB()
	tls := model.Tls{Id: 1, Name: "public-trust", Server: json.RawMessage(`{"enabled":true,"server_name":"edge.example"}`), Client: json.RawMessage(`{}`)}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	// Empty historical templates must be derived without modifying their rows.
	rows := []model.Inbound{
		{Id: 1, Type: "mieru", Tag: "mieru", Options: json.RawMessage(`{"listen_port":2443,"transport":"TCP","mtu":1400}`), Addrs: json.RawMessage(`[{"server":"edge.example","server_port":2443}]`), OutJson: json.RawMessage(`{}`)},
		{Id: 2, Type: "trusttunnel", Tag: "trust", TlsId: 1, Options: json.RawMessage(`{"listen_port":2444,"network":"udp"}`), Addrs: json.RawMessage(`[{"server":"edge.example","server_port":2444}]`), OutJson: json.RawMessage(`{}`)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	password := "synthetic-private-delivery"
	mutation := map[string]any{"password": map[string]string{"intent": "replace", "value": password}}
	config, _ := json.Marshal(map[string]any{"mieru": mutation, "trusttunnel": mutation})
	client := model.Client{Name: "native-client", Enable: true, Config: config, Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`)}
	raw, _ := json.Marshal(client)
	if _, err := entityclients.Save(entityclients.SaveRequest{Tx: db, Action: "new", Data: raw}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(client.Config)
	if bytes.Contains(before, []byte(password)) {
		t.Fatal("canonical credential was not sealed")
	}
	body, _, err := (&JSONService{}).GetJSON(client.SubSecret)
	if err != nil || body == nil {
		t.Fatal("private native JSON was not delivered", err)
	}
	var delivered struct {
		Outbounds []struct {
			Type, Username, Password string
			Transport                any
			QUIC                     bool `json:"quic"`
		}
	}
	if json.Unmarshal([]byte(*body), &delivered) != nil {
		t.Fatal("private response was not JSON")
	}
	found := 0
	for _, outbound := range delivered.Outbounds {
		if outbound.Type != "mieru" && outbound.Type != "trusttunnel" {
			continue
		}
		found++
		if outbound.Username != client.Name || outbound.Password != password {
			t.Fatal("delivered principal/credential differs from its owner")
		}
		if outbound.Type == "mieru" && outbound.Transport != "TCP" {
			t.Fatal("Mieru transport changed")
		}
		if outbound.Type == "trusttunnel" && !outbound.QUIC {
			t.Fatal("UDP-only TrustTunnel was delivered as TCP")
		}
	}
	if found != 2 {
		t.Fatal("native endpoint was omitted")
	}
	if _, _, err := (&XrayService{}).GetXray(client.SubSecret); err == nil {
		t.Fatal("unsupported target silently changed a native endpoint")
	}
	if err := db.First(&client, client.Id).Error; err != nil || !bytes.Equal(before, client.Config) {
		t.Fatal("delivery modified canonical credentials")
	}
	for _, original := range rows {
		var current model.Inbound
		if err := db.First(&current, original.Id).Error; err != nil || !bytes.Equal(original.OutJson, current.OutJson) {
			t.Fatal("delivery modified a historical template")
		}
	}
}
