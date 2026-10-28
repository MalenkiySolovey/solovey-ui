//go:build shtorm_extended && with_quic

package sub

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
)

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
