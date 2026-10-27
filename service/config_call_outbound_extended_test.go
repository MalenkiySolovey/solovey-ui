//go:build shtorm_extended

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

func TestCallOutboundCandidateOwnsPrivateCredentialsTransactionally(t *testing.T) {
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
	before := state()
	const join = "https://dion.vc/event/private-outbound-room-canary"
	const cookies = `[{"name":"session","value":"private-outbound-cookie-canary"}]`
	replace := func(value string) map[string]string { return map[string]string{"intent": "replace", "value": value} }
	draft := map[string]any{"type": "call", "tag": "owned-call-outbound", "platform": "private-invalid-platform", "join_link": replace(join), "cookies": replace(cookies)}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid Call outbound candidate was not privately rejected", err)
	}
	if state() != before || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
		t.Fatal("failed Call outbound candidate committed identity, secret, audit, Change or effects")
	}
	draft["platform"], draft["detour"] = "dion", "direct"
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("outbounds", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	config, err := svc.GetConfig("")
	if err != nil || singboxvalidation.ValidateConfig(*config) != nil || !bytes.Contains(*config, []byte(join)) || !bytes.Contains(*config, []byte("private-outbound-cookie-canary")) || bytes.Contains(*config, []byte("sbox:v1:")) {
		t.Fatal("trusted runtime lost its outbound credential binding", err)
	}
	var row model.Outbound
	if err := db.Where("tag = ?", draft["tag"]).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	read, err := entityoutbounds.GetAll(db)
	masked, _ := json.Marshal(read)
	if err != nil || bytes.Contains(masked, []byte("canary")) || bytes.Contains(masked, []byte("sbox:v1:")) || bytes.Contains(row.Options, []byte("canary")) {
		t.Fatal("normal outbound read or storage disclosed its private values")
	}
	before = state()
	draft["id"], draft["join_link"] = row.Id, map[string]string{"intent": "clear"}
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("outbounds", "edit", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_OUTBOUND_CANDIDATE_REJECTED") || state() != before {
		t.Fatal("clearing the required room committed a partial candidate", err)
	}
}
