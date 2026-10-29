//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestHysteria2RealmOutboundUsesCanonicalCredentialsAndPrivateAdmission(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	const token = "private-realm-outbound-token-canary"
	realm := map[string]any{"server_url": ":private-invalid-url-canary", "realm_id": "native-realm", "token": map[string]string{"intent": "replace", "value": token}, "stun_servers": []string{"127.0.0.1:3478"}, "ip_version": 4}
	draft := map[string]any{"type": "hysteria2", "tag": "realm-client", "realm": realm, "password": "protocol-authentication", "tls": map[string]any{"enabled": true}}
	save := func(action string, value any) error {
		raw, _ := json.Marshal(value)
		_, err := svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
		return err
	}
	if err := save("new", draft); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid Realm candidate was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "realm-client").Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("rejected Realm candidate committed effects", err)
	}
	realm["server_url"] = "https://127.0.0.1:8443"
	if err := save("new", draft); err != nil {
		t.Fatal(err)
	}
	var stored model.Outbound
	if err := db.Where("tag = ?", "realm-client").First(&stored).Error; err != nil || bytes.Contains(stored.Options, []byte(token)) {
		t.Fatal("Realm token was not sealed", err)
	}
	views, err := entityoutbounds.GetAll(db)
	masked, _ := json.Marshal(views)
	if err != nil || bytes.Contains(masked, []byte(token)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("Realm read disclosed a credential", err)
	}
	var rows []map[string]any
	_ = json.Unmarshal(masked, &rows)
	var editable map[string]any
	for _, row := range rows {
		if row["tag"] == "realm-client" {
			editable = row
		}
	}
	if editable == nil {
		t.Fatal("Realm resource disappeared")
	}
	nested := editable["realm"].(map[string]any)
	view := nested["token"].(map[string]any)
	if view["present"] != true {
		t.Fatal("Realm token presence missing")
	}
	nested["token"] = map[string]any{"intent": "unchanged", "reference": view["reference"], "present": view["present"], "rotationRevision": view["rotationRevision"]}
	nested["realm_id"] = "renamed-realm"
	if err := save("edit", editable); err != nil {
		t.Fatal("Realm Keep edit failed", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || !bytes.Contains(*config, []byte(token)) || !bytes.Contains(*config, []byte("renamed-realm")) || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("Realm runtime projection lost owned semantics", err)
	}
	foreign := map[string]any{"type": "hysteria2", "tag": "realm-copy", "realm": nested, "tls": map[string]any{"enabled": true}}
	if err := save("new", foreign); err == nil {
		t.Fatal("another resource borrowed a Realm token")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), stored.Options...)
	nested["TOKEN"] = map[string]string{"intent": "replace", "value": "private-alias-canary"}
	if err := save("edit", editable); err == nil {
		t.Fatal("case alias bypassed Realm ownership")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil || !bytes.Equal(before, stored.Options) {
		t.Fatal("rejected alias rewrote desired state", err)
	}
}

func TestNativeOpenConnectCandidateUsesCanonicalOwnerAndDNSReferences(t *testing.T) {
	if !registry.Resolve("endpoints", "openconnect").Available() {
		t.Skip("OpenConnect is absent from this profile")
	}
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	const secret = "private-openconnect-candidate-canary"
	draft := map[string]any{"type": "openconnect", "tag": "native-vpn", "server": "https://192.0.2.1", "flavor": "invalid-canary", "username": "fixture", "password": map[string]string{"intent": "replace", "value": secret}}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid native endpoint was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("rejected endpoint committed identity or runtime effects", err)
	}
	draft["flavor"] = "anyconnect"
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	view, err := entityendpoints.GetAll(db)
	masked, _ := json.Marshal(view)
	if err != nil || bytes.Contains(masked, []byte(secret)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("endpoint API disclosed credentials", err)
	}
	base := NewSingBoxBaseConfigStore(nil)
	valid := json.RawMessage(`{"log":{"disabled":true},"dns":{"servers":[{"type":"openconnect","tag":"vpn-dns","endpoint":"native-vpn","accept_default_resolvers":true,"accept_search_domain":true}],"final":"vpn-dns"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
	if err := base.Save(db, valid); err != nil {
		t.Fatal("canonical OpenConnect DNS candidate rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || !bytes.Contains(*config, []byte(secret)) || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("native endpoint/DNS runtime projection failed", err)
	}
	invalid := bytes.ReplaceAll(valid, []byte(`"endpoint":"native-vpn"`), []byte(`"endpoint":"missing-vpn"`))
	if err := base.Save(db, invalid); err == nil {
		t.Fatal("missing native DNS endpoint committed")
	}
	if _, err := svc.Save("endpoints", "del", json.RawMessage(`"native-vpn"`), "", "admin", "fixture.invalid"); err == nil {
		t.Fatal("referenced native endpoint was removed")
	}
}

func TestNativeOutboundCandidateOwnsCredentialsTransactionally(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel", "sudoku"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			svc, applier, lifecycle := newHotReloadConfigService(t)
			svc.setLastUpdate(12345)
			state := func() string {
				t.Helper()
				var counts []int64
				for _, record := range []any{&model.Outbound{}, &model.AuditEvent{}, &model.Setting{}, &model.Changes{}} {
					var count int64
					if err := db.Model(record).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					counts = append(counts, count)
				}
				raw, _ := json.Marshal(counts)
				return string(raw)
			}
			const password = "private-native-outbound-password-canary"
			draft := map[string]any{"type": kind, "tag": "owned-" + kind, "server": "192.0.2.1", "server_port": 443, "username": "fixture-user", "password": map[string]string{"intent": "replace", "value": password}, "detour": "direct"}
			field := "password"
			if kind == "mieru" {
				draft["transport"] = "private-invalid-transport"
			} else if kind == "sudoku" {
				field = "key"
				delete(draft, "password")
				delete(draft, "username")
				draft[field] = map[string]string{"intent": "replace", "value": password}
				draft["multiplex"] = "private-invalid-multiplex"
			} else {
				draft["tls"] = map[string]any{"enabled": true}
				draft["username"] = ""
			}
			before := state()
			raw, _ := json.Marshal(draft)
			if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
				t.Fatal("invalid native outbound was not privately rejected", err)
			}
			if state() != before || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
				t.Fatal("failed native outbound committed identity, secret, audit, Change or effects")
			}
			if kind == "mieru" {
				draft["transport"] = "TCP"
				draft["multiplexing"] = "MULTIPLEXING_LOW"
				draft["handshake_mode"] = "HANDSHAKE_STANDARD"
			} else if kind == "sudoku" {
				draft["multiplex"] = "off"
				draft["http_mask"] = map[string]any{"enabled": false}
			} else {
				draft["username"] = "fixture-user"
				draft["multiplex"] = map[string]any{"enabled": false, "max_streams": 8}
			}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal(err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil || !bytes.Contains(*config, []byte(password)) || bytes.Contains(*config, []byte("sbox:v1:")) {
				t.Fatal("trusted native outbound projection lost the password", err)
			}
			var row model.Outbound
			if err := db.Where("tag = ?", draft["tag"]).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			view, err := entityoutbounds.GetAll(db)
			masked, _ := json.Marshal(view)
			if err != nil || bytes.Contains(masked, []byte(password)) || bytes.Contains(masked, []byte("sbox:v1:")) || bytes.Contains(row.Options, []byte(password)) {
				t.Fatal("native outbound storage or normal read disclosed its password")
			}
			var ref map[string]any
			var normal []map[string]any
			if json.Unmarshal(masked, &normal) != nil {
				t.Fatal("invalid normal Outbounds representation")
			}
			for _, entry := range normal {
				if entry["tag"] == draft["tag"] {
					ref, _ = entry[field].(map[string]any)
				}
			}
			if ref == nil || ref["present"] != true {
				t.Fatal("native password has no masked reference")
			}
			draft["id"], draft[field] = row.Id, map[string]any{"intent": "unchanged", "reference": ref["reference"], "present": ref["present"], "rotationRevision": ref["rotationRevision"]}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "edit", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal("masked edit lost password ownership", err)
			}
			before = state()
			draft[field] = map[string]string{"intent": "clear"}
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("outbounds", "edit", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || state() != before {
				t.Fatal("clearing the required native password committed a partial candidate", err)
			}
		})
	}
}
