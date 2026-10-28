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
