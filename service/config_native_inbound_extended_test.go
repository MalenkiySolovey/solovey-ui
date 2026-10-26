//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestSSHMTProxyCandidateValidationAndRollback(t *testing.T) {
	for _, kind := range []string{"ssh", "mtproxy"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			client := model.Client{Id: 7, Name: "native-assigned", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-subscription"}
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
			draft := map[string]any{"type": kind, "tag": "native-" + kind, "listen": "127.0.0.1", "listen_port": 23456}
			if kind == "ssh" {
				_, key := makeSelfSignedCertPEM(t, time.Now().Add(time.Hour))
				draft["server_version"], draft["max_auth_tries"] = "SSH-2.0-SoloveyFixture", 3
				draft["host_key"] = map[string]string{"intent": "replace", "value": string(key)}
				draft["host_key_path"] = []string{"/__nonexistent_solovey_fixture__/private-invalid-canary"}
			} else {
				draft["prefer_ip"] = "private-invalid-canary"
			}
			raw, _ := json.Marshal(draft)
			if _, err := svc.Save("inbounds", "new", raw, "7", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "private-invalid-canary") {
				t.Fatal("actual provider did not safely reject the native candidate", err)
			}
			var after model.Client
			if err := db.First(&after, client.Id).Error; err != nil || !bytes.Equal(after.Config, client.Config) || !bytes.Equal(after.Inbounds, client.Inbounds) {
				t.Fatal("failed candidate changed Users state")
			}
			if count(&model.Inbound{}) != 0 || count(&model.AuditEvent{}) != audits || count(&model.Setting{}) != settings || count(&model.Changes{}) != changes || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
				t.Fatal("failed native candidate committed identity, master, audit, change or effects")
			}
			delete(draft, "host_key_path")
			if kind == "mtproxy" {
				for field, value := range map[string]any{"prefer_ip": "only-ipv4", "concurrency": 4, "auto_update": false,
					"domain_fronting_host": "fixture.invalid", "domain_fronting_port": 8443, "domain_fronting_proxy_protocol": true,
					"allow_fallback_on_unknown_dc": true, "idle_timeout": "2m", "handshake_timeout": "8s", "tolerate_time_skewness": "30s",
					"throttle_check_interval": "45s", "throttle_max_connections": 20, "doppelganger_each": "2h",
					"doppelganger_urls": []string{"https://fixture.invalid/"}, "doppelganger_per_raid": 2, "doppelganger_drs": true} {
					draft[field] = value
				}
			}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("inbounds", "new", raw, "7", "admin", "fixture.invalid"); err != nil {
				t.Fatal("valid native candidate was rejected", err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil {
				t.Fatal("native saved candidate failed the exact provider check", err)
			}
			var root struct{ Inbounds []map[string]json.RawMessage }
			if json.Unmarshal(*config, &root) != nil || len(root.Inbounds) != 1 {
				t.Fatal("native runtime projection lost its inbound")
			}
			for field, value := range draft {
				if field == "host_key" {
					continue
				}
				want, _ := json.Marshal(value)
				var projected bytes.Buffer
				if err := json.Compact(&projected, root.Inbounds[0][field]); err != nil || !bytes.Equal(projected.Bytes(), want) {
					t.Fatalf("native field %s did not survive storage and runtime projection", field)
				}
			}
			var inbound model.Inbound
			if err := db.First(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			normal, err := entityinbounds.GetByIDs(db, strconv.Itoa(int(inbound.Id)))
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(normal)
			if bytes.Contains(encoded, []byte("PRIVATE KEY")) || bytes.Contains(encoded, []byte("sbox:v1:")) {
				t.Fatal("normal native read disclosed host-key material")
			}
			if len(applier.calls) != 1 || len(lifecycle.calls) != 0 {
				t.Fatal("native save bypassed the existing apply owner")
			}
		})
	}
}

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

func TestNativeCandidateFinalUserDisableKeepsCredentialAndReenables(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	client := model.Client{Id: 7, Name: "last-native-user", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-subscription"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	svc, applier, lifecycle := newHotReloadConfigService(t)
	if _, err := svc.Save("inbounds", "new", json.RawMessage(`{"type":"mieru","tag":"last-user-mieru","listen":"127.0.0.1","listen_port":23456,"transport":"TCP","mtu":1400}`), "7", "admin", "fixture.example"); err != nil {
		t.Fatal("native assignment failed", err)
	}
	if err := db.First(&client, 7).Error; err != nil {
		t.Fatal(err)
	}
	sealed := bytes.Clone(client.Config)
	var membership []uint
	if json.Unmarshal(client.Inbounds, &membership) != nil || len(membership) != 1 {
		t.Fatal("native assignment membership is invalid")
	}
	initial, err := entityclients.GetByID(db, "7")
	if err != nil || len(*initial) != 1 {
		t.Fatal("native credential projection is unavailable", err)
	}
	reference := bytes.Clone((*initial)[0].Config)
	for _, enabled := range []bool{false, true} {
		projected, err := entityclients.GetByID(db, "7")
		if err != nil || len(*projected) != 1 {
			t.Fatal("native user could not reload", err)
		}
		(*projected)[0].Enable = enabled
		var draft map[string]map[string]any
		if json.Unmarshal((*projected)[0].Config, &draft) != nil {
			t.Fatal("native credential draft is invalid")
		}
		password := draft["mieru"]["password"].(map[string]any)
		draft["mieru"]["password"] = map[string]any{"intent": "unchanged", "reference": password["reference"], "present": password["present"], "rotationRevision": password["rotationRevision"]}
		(*projected)[0].Config, err = json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal((*projected)[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Save("clients", "edit", data, "", "admin", "fixture.example"); err != nil {
			t.Fatal("last native user disable/re-enable was rejected", err)
		}
		var stored model.Client
		if err := db.First(&stored, 7).Error; err != nil {
			t.Fatal(err)
		}
		var storedMembership []uint
		if stored.Enable != enabled || !bytes.Equal(stored.Config, sealed) || json.Unmarshal(stored.Inbounds, &storedMembership) != nil || !reflect.DeepEqual(storedMembership, membership) {
			t.Fatal("user state change lost membership or changed sealed credentials")
		}
		reloaded, err := entityclients.GetByID(db, "7")
		if err != nil || len(*reloaded) != 1 || !bytes.Equal((*reloaded)[0].Config, reference) {
			t.Fatal("user state change rotated or exposed the credential reference", err)
		}
		raw, err := svc.GetConfig("")
		if err != nil || singboxvalidation.ValidateConfig(*raw) != nil {
			t.Fatal("last-user configuration failed the pinned provider check", err)
		}
		var config struct {
			Inbounds []struct {
				Tag   string
				Users []json.RawMessage
			}
		}
		if json.Unmarshal(*raw, &config) != nil || len(config.Inbounds) != 1 || config.Inbounds[0].Tag != "last-user-mieru" || len(config.Inbounds[0].Users) != map[bool]int{false: 0, true: 1}[enabled] {
			t.Fatal("effective users did not follow the saved enabled state")
		}
	}
	if len(applier.calls) != 3 || len(lifecycle.calls) != 0 {
		t.Fatal("user state change bypassed the existing apply owner")
	}
}
