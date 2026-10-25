//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestNativeCandidateRejectionRollsBackAssignmentSecretsAndEffects(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	client := model.Client{Id: 7, Name: "alice", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-subscription"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc, applier, lifecycle := newHotReloadConfigService(t)
	svc.setLastUpdate(12345)
	count := func(record any) int64 {
		t.Helper()
		var value int64
		if err := db.Model(record).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	auditBefore, settingsBefore, changesBefore := count(&model.AuditEvent{}), count(&model.Setting{}), count(&model.Changes{})
	bad := json.RawMessage(`{"type":"mieru","tag":"native","listen":"127.0.0.1","listen_port":23456,"transport":"TCP","traffic_pattern":"private-pattern-canary"}`)
	if _, err := svc.Save("inbounds", "new", bad, "7", "admin", "fixture.example"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "private-pattern-canary") {
		t.Fatal("native save did not reject the actual provider candidate safely", err)
	}
	var stored model.Client
	if err := db.First(&stored, 7).Error; err != nil || !bytes.Equal(stored.Config, client.Config) || !bytes.Equal(stored.Inbounds, client.Inbounds) {
		t.Fatal("failed candidate changed the assigned user's credentials or membership")
	}
	if count(&model.Inbound{}) != 0 || count(&model.AuditEvent{}) != auditBefore || count(&model.Setting{}) != settingsBefore || count(&model.Changes{}) != changesBefore || svc.getLastUpdate() != 12345 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("failed provider candidate committed identity, key, audit, change, or live effects")
	}
	good := json.RawMessage(`{"type":"mieru","tag":"native","listen":"127.0.0.1","listen_port":23456,"transport":"TCP","user_hint_is_mandatory":true,"mtu":1400}`)
	if _, err := svc.Save("inbounds", "new", good, "7", "admin", "fixture.example"); err != nil {
		t.Fatal("valid native candidate was rejected", err)
	}
	raw, err := svc.GetConfig("")
	if err != nil || singboxvalidation.ValidateConfig(*raw) != nil {
		t.Fatal("saved native candidate did not survive the pinned provider dry check", err)
	}
	projected, err := entityclients.GetByID(db, "7")
	if err != nil || len(*projected) != 1 {
		t.Fatal("saved native user could not reload")
	}
	var credential struct {
		Mieru struct {
			Password struct{ Present bool }
		}
	}
	if json.Unmarshal((*projected)[0].Config, &credential) != nil || !credential.Mieru.Password.Present || bytes.Contains((*projected)[0].Config, []byte("sbox:v1:")) {
		t.Fatal("normal user read did not mask the saved credential")
	}
	if len(applier.calls) != 1 || len(lifecycle.calls) != 0 || svc.getLastUpdate() == 12345 {
		t.Fatal("valid native save bypassed the existing apply effects")
	}
}
