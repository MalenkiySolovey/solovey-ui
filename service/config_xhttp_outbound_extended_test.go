//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
)

func TestXHTTPDownloadCredentialsAndReferences(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	svc, _, _ := newHotReloadConfigService(t)
	save := func(action string, value any) error {
		raw, _ := json.Marshal(value)
		_, err := svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
		return err
	}
	if err := save("new", map[string]any{"type": "direct", "tag": "download-owner"}); err != nil {
		t.Fatal(err)
	}
	cert, key := makeSelfSignedCertPEM(t, time.Now().Add(time.Hour))
	tls := map[string]any{"enabled": true, "insecure": true, "client_certificate": []string{string(cert)}, "client_key": map[string]string{"intent": "replace", "value": string(key)}}
	download := map[string]any{"server": "127.0.0.1", "server_port": 443, "x_padding_bytes": "100-1000", "detour": "download-owner", "tls": tls}
	draft := map[string]any{"type": "vless", "tag": "xhttp-owner", "uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382", "server": "127.0.0.1", "server_port": 443,
		"transport": map[string]any{"type": "xhttp", "mode": "packet-up", "x_padding_bytes": "100-1000", "download": download}}
	if err := save("new", draft); err != nil {
		t.Fatal("valid split download candidate failed", err)
	}
	var row model.Outbound
	if err := db.Where("tag = ?", "xhttp-owner").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	draft["id"] = row.Id
	before := append([]byte(nil), row.Options...)
	if bytes.Contains(before, []byte("PRIVATE KEY")) {
		t.Fatal("download private key was stored without its owner envelope")
	}
	view, err := entityoutbounds.GetAll(db)
	encoded, _ := json.Marshal(view)
	if err != nil || bytes.Contains(encoded, []byte("PRIVATE KEY")) || bytes.Contains(encoded, []byte("sbox:v1:")) {
		t.Fatal("normal read disclosed download credential material")
	}
	var normal []map[string]any
	_ = json.Unmarshal(encoded, &normal)
	var secret map[string]any
	for _, value := range normal {
		if value["tag"] == "xhttp-owner" {
			transport := value["transport"].(map[string]any)
			secret = transport["download"].(map[string]any)["tls"].(map[string]any)["client_key"].(map[string]any)
		}
	}
	if secret == nil || secret["present"] != true {
		t.Fatal("download key has no masked row-bound reference")
	}
	secret = map[string]any{"intent": "unchanged", "reference": secret["reference"], "present": secret["present"], "rotationRevision": secret["rotationRevision"]}
	tls["client_key"] = secret
	if err := save("edit", draft); err != nil {
		t.Fatal("masked edit lost download key ownership", err)
	}
	if err := db.First(&row, row.Id).Error; err != nil || !bytes.Equal(before, row.Options) {
		t.Fatal("unchanged edit rotated the download key")
	}
	config, err := svc.GetConfig("")
	var runtime struct {
		Outbounds []struct {
			Tag       string
			Transport struct {
				Download struct {
					TLS struct {
						ClientKey string `json:"client_key"`
					}
				}
			}
		}
	}
	if err != nil || json.Unmarshal(*config, &runtime) != nil || bytes.Contains(*config, []byte("sbox:v1:")) {
		t.Fatal("trusted config did not resolve the download key", err)
	}
	found := false
	for _, outbound := range runtime.Outbounds {
		if outbound.Tag == "xhttp-owner" {
			found = outbound.Transport.Download.TLS.ClientKey == string(key)
		}
	}
	if !found {
		t.Fatal("trusted config changed the download key")
	}
	tls["client_key"] = map[string]string{"intent": "replace", "value": "invalid-private-key"}
	if err := save("edit", draft); err == nil || bytes.Contains([]byte(err.Error()), []byte("invalid-private-key")) {
		t.Fatal("invalid native TLS pair was not privately rejected")
	}
	if err := db.First(&row, row.Id).Error; err != nil || !bytes.Equal(before, row.Options) {
		t.Fatal("failed native TLS validation changed the owner")
	}
	tls["client_key"] = secret
	download["detour"] = "xhttp-owner"
	if err := save("edit", draft); err == nil {
		t.Fatal("cyclic split download dependency was committed")
	}
	download["detour"] = "missing-owner"
	if err := save("edit", draft); err == nil {
		t.Fatal("missing split download dependency was committed")
	}
	if err := save("del", "download-owner"); err == nil {
		t.Fatal("captured split download dependency was deleted")
	}
}
