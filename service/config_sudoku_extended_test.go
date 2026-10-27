//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestSudokuCompleteCandidateRejectsBeforeOwnerCommit(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	client := model.Client{Id: 7, Name: "sudoku-assigned", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-sudoku-subscription"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc, applier, lifecycle := newHotReloadConfigService(t)
	svc.setLastUpdate(12345)
	count := func(record any) int64 {
		var value int64
		if err := db.Model(record).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	audits, settings, changes := count(&model.AuditEvent{}), count(&model.Setting{}), count(&model.Changes{})
	master, _ := credentials.GenerateSudokuMaster()
	draft := map[string]any{"type": "sudoku", "tag": "owned-sudoku", "listen": "127.0.0.1", "listen_port": 23462,
		"master_key": map[string]string{"intent": "replace", "value": master}, "http_mask_mode": "private-invalid-canary"}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("inbounds", "new", raw, "7", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "private-invalid-canary") || strings.Contains(err.Error(), master) {
		t.Fatal("invalid Sudoku protocol candidate was not safely rejected", err)
	}
	var after model.Client
	if err := db.First(&after, client.Id).Error; err != nil || !bytes.Equal(after.Config, client.Config) || !bytes.Equal(after.Inbounds, client.Inbounds) {
		t.Fatal("failed candidate changed Users")
	}
	if count(&model.Inbound{}) != 0 || count(&model.AuditEvent{}) != audits || count(&model.Setting{}) != settings || count(&model.Changes{}) != changes || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
		t.Fatal("failed candidate committed identity, secret, audit, Change or effects")
	}
	for field, value := range map[string]any{"aead_method": "aes-128-gcm", "padding_min": 0, "padding_max": 30, "table_type": "up_ascii_down_entropy", "handshake_timeout": 0,
		"enable_pure_downlink": false, "custom_table": "xpxvvpvv", "custom_tables": []string{"xpxvvpvv"}, "disable_http_mask": false,
		"http_mask_mode": "poll", "path_root": "owned", "fallback": "fixture.invalid:8080"} {
		draft[field] = value
	}
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("inbounds", "new", raw, "7", "admin", "fixture.invalid"); err != nil {
		t.Fatal("valid Sudoku candidate rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || singboxvalidation.ValidateConfig(*config) != nil || bytes.Contains(*config, []byte(master)) || bytes.Contains(*config, []byte("master_key")) || bytes.Contains(*config, []byte("sbox:v1:")) {
		t.Fatal("runtime projection lost its public-only contract", err)
	}
	var root struct{ Inbounds []map[string]json.RawMessage }
	if json.Unmarshal(*config, &root) != nil || len(root.Inbounds) != 1 || len(root.Inbounds[0]["users"]) != 0 {
		t.Fatal("Sudoku acquired engine Users")
	}
	public, _ := credentials.SudokuPublicKey(master)
	var projectedPublic string
	_ = json.Unmarshal(root.Inbounds[0]["key"], &projectedPublic)
	if projectedPublic != public {
		t.Fatal("wrong serving public identity")
	}
	for field, value := range draft {
		if field == "master_key" {
			continue
		}
		want, _ := json.Marshal(value)
		var projected bytes.Buffer
		if json.Compact(&projected, root.Inbounds[0][field]) != nil || !bytes.Equal(projected.Bytes(), want) {
			t.Fatalf("Sudoku field %s did not survive runtime projection", field)
		}
	}
	var row model.Inbound
	if err := db.Where("tag = ?", draft["tag"]).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	read, err := entityinbounds.GetByIDs(db, strconv.Itoa(int(row.Id)))
	encoded, _ := json.Marshal(read)
	if err != nil || bytes.Contains(encoded, []byte(master)) || bytes.Contains(encoded, []byte("sbox:v1:")) {
		t.Fatal("normal Inbounds read disclosed private state")
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	sealedClient := bytes.Clone(client.Config)
	for _, enabled := range []bool{false, true} {
		client.Enable = enabled
		client.Config = json.RawMessage(`{}`)
		data, _ := json.Marshal(client)
		if _, err := svc.Save("clients", "edit", data, "", "admin", "fixture.invalid"); err != nil {
			t.Fatal("client metadata edit failed", err)
		}
		if err := db.First(&after, client.Id).Error; err != nil || !bytes.Equal(after.Config, sealedClient) {
			t.Fatal("client enable state changed delivery keys")
		}
		candidate, err := svc.GetConfig("")
		if err != nil || !bytes.Equal(*candidate, *config) {
			t.Fatal("Panel client enable changed keyless protocol authentication")
		}
	}
	projected, err := entityclients.GetByID(db, "7")
	if err != nil || len(*projected) != 1 || bytes.Contains((*projected)[0].Config, []byte("sbox:v1:")) {
		t.Fatal("normal Users read disclosed split keys")
	}
}
