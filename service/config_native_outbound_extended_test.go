//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestNativeLimitersUseCanonicalRoutesAndTransactionalCandidates(t *testing.T) {
	for _, kind := range []string{"bandwidth-limiter", "connection-limiter", "rate-limiter", "traffic-limiter"} {
		t.Run(kind, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			svc, _, _ := newHotReloadConfigService(t)
			draft := map[string]any{"type": kind, "tag": "limit", "route": map[string]any{"final": "direct"}, "strategy": "global", "mode": "bidirectional", "speed": "10MB", "total": "1MB", "count": 4, "interval": "1s"}
			switch kind {
			case "bandwidth-limiter":
				delete(draft, "total")
				delete(draft, "count")
				delete(draft, "interval")
			case "traffic-limiter":
				delete(draft, "speed")
				delete(draft, "count")
				delete(draft, "interval")
			default:
				delete(draft, "speed")
				delete(draft, "total")
				delete(draft, "mode")
				if kind == "connection-limiter" {
					draft["strategy"] = "connection"
					delete(draft, "interval")
				} else {
					draft["strategy"] = "token-bucket"
				}
			}
			save := func(action string) error {
				raw, _ := json.Marshal(draft)
				_, err := svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
				return err
			}
			for _, invalid := range []string{"absent", "limit"} {
				draft["route"] = map[string]any{"final": invalid}
				if err := save("new"); err == nil {
					t.Fatal("missing/self final was admitted")
				}
			}
			draft["route"] = map[string]any{"final": "direct"}
			if err := save("new"); err != nil {
				t.Fatal("valid native limiter", err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil || !bytes.Contains(*config, []byte(kind)) {
				t.Fatal("native limiter missing from validated runtime candidate", err)
			}
			var row model.Outbound
			if err := dbsqlite.DB().Where("tag = ?", "limit").First(&row).Error; err != nil {
				t.Fatal(err)
			}
			before := string(row.Options)
			draft["id"], draft["strategy"] = row.Id, "manager"
			if err := save("edit"); err == nil {
				t.Fatal("foreign manager became a live User authority")
			}
			if err := dbsqlite.DB().First(&row, row.Id).Error; err != nil || string(row.Options) != before {
				t.Fatal("rejected edit changed desired state", err)
			}
			if _, err := svc.Save("outbounds", "edit", json.RawMessage(`{"id":1,"type":"direct","tag":"renamed"}`), "", "admin", "fixture.invalid"); err == nil {
				t.Fatal("referenced final outbound was renamed")
			}
		})
	}
}

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

func TestNativeRealmServiceCandidatePreservesPrivateTokens(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	svc, applier, lifecycle := newHotReloadConfigService(t)
	const token = "private-realm-candidate-canary"
	draft := map[string]any{"type": "hysteria-realm", "tag": "rendezvous", "listen": "127.0.0.1", "listen_port": 9899,
		"users":        []any{map[string]any{"name": "operator", "max_realms": 2, "token": map[string]string{"intent": "replace", "value": token}}},
		"idle_timeout": "invalid-private-canary"}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("services", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_SERVICE_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid realm candidate was not privately rejected", err)
	}
	var count int64
	if err := db.Model(&model.Service{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
		t.Fatal("rejected realm committed identity or effects", err)
	}
	draft["idle_timeout"] = "15s"
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("services", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	view, err := entityservices.GetAll(db)
	masked, _ := json.Marshal(view)
	if err != nil || bytes.Contains(masked, []byte(token)) || bytes.Contains(masked, []byte("sbox:v1:")) {
		t.Fatal("realm API disclosed credentials", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || !bytes.Contains(*config, []byte(token)) || singboxvalidation.ValidateConfig(*config) != nil {
		t.Fatal("realm runtime projection failed", err)
	}
	edit, _ := json.Marshal((*view)[0])
	var fields map[string]any
	_ = json.Unmarshal(edit, &fields)
	fields["users"].([]any)[0].(map[string]any)["token"].(map[string]any)["intent"] = "unchanged"
	delete(fields["users"].([]any)[0].(map[string]any)["token"].(map[string]any), "correlation")
	edit, _ = json.Marshal(fields)
	if _, err := svc.Save("services", "edit", edit, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal("masked realm edit failed", err)
	}
}

func TestNativeOpenVPNClientCandidateUsesCanonicalOwnerAndDNSReferences(t *testing.T) {
	if !registry.Resolve("endpoints", "openvpn-client").Available() {
		t.Skip("OpenVPN client is absent from this profile")
	}
	if registry.Resolve("endpoints", "openvpn-server").Available() {
		t.Fatal("client integration advertised an unowned serving workflow")
	}
	for _, mode := range []string{"tls", "static_key"} {
		t.Run(mode, func(t *testing.T) {
			initSettingTestDB(t)
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
			db := dbsqlite.DB()
			svc, applier, lifecycle := newHotReloadConfigService(t)
			const password = "private-openvpn-candidate-canary"
			draft := map[string]any{"type": "openvpn-client", "tag": "native-openvpn", "mode": mode, "network": "udp", "servers": []map[string]any{{"server": "192.0.2.1", "server_port": 1194}}}
			if mode == "tls" {
				cert, key := makeSelfSignedCertPEM(t, time.Now().Add(time.Hour))
				draft["username"] = "fixture"
				draft["password"] = map[string]string{"intent": "replace", "value": password}
				draft["tls"] = map[string]any{"certificate": []string{string(cert)}, "client_certificate": []string{string(cert)}, "client_key": map[string]string{"intent": "replace", "value": string(key)}}
				draft["data_ciphers"] = []string{"AES-256-GCM"}
			} else {
				draft["address"] = []string{"10.8.0.2/24"}
				draft["peer_address"] = "10.8.0.1"
				draft["cipher"] = "AES-256-CBC"
				draft["static_key"] = map[string]string{"intent": "replace", "value": "-----BEGIN OpenVPN Static key V1-----\n" + strings.Repeat("01", 256) + "\n-----END OpenVPN Static key V1-----"}
			}
			draft["network"] = "private-invalid-network-canary"
			raw, _ := json.Marshal(draft)
			if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_ENDPOINT_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
				t.Fatal("invalid client candidate was not privately rejected", err)
			}
			var count int64
			if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil || count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 {
				t.Fatal("failed client candidate committed state or runtime effects", err)
			}
			draft["network"] = "udp"
			raw, _ = json.Marshal(draft)
			if _, err := svc.Save("endpoints", "new", raw, "", "admin", "fixture.invalid"); err != nil {
				t.Fatal("valid native client rejected", err)
			}
			view, err := entityendpoints.GetAll(db)
			masked, _ := json.Marshal(view)
			if err != nil || bytes.Contains(masked, []byte(password)) || bytes.Contains(masked, []byte("sbox:v1:")) || bytes.Contains(masked, []byte("BEGIN OpenVPN Static key")) {
				t.Fatal("endpoint read disclosed credentials", err)
			}
			base := NewSingBoxBaseConfigStore(nil)
			valid := json.RawMessage(`{"log":{"disabled":true},"dns":{"servers":[{"type":"openvpn","tag":"vpn-dns","endpoint":"native-openvpn","accept_default_resolvers":true,"accept_search_domain":true}],"final":"vpn-dns"},"outbounds":[{"type":"direct","tag":"direct"}]}`)
			if err := base.Save(db, valid); err != nil {
				t.Fatal("native client DNS reference rejected", err)
			}
			config, err := svc.GetConfig("")
			if err != nil || singboxvalidation.ValidateConfig(*config) != nil {
				t.Fatal("native client/DNS runtime projection failed", err)
			}
			if mode == "tls" && !bytes.Contains(*config, []byte(password)) {
				t.Fatal("native password lost owner binding")
			}
			invalid := bytes.ReplaceAll(valid, []byte(`"endpoint":"native-openvpn"`), []byte(`"endpoint":"missing-vpn"`))
			if err := base.Save(db, invalid); err == nil {
				t.Fatal("missing client DNS endpoint committed")
			}
			if _, err := svc.Save("endpoints", "del", json.RawMessage(`"native-openvpn"`), "", "admin", "fixture.invalid"); err == nil {
				t.Fatal("referenced VPN endpoint was removed")
			}
		})
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
