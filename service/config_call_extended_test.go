//go:build shtorm_extended

package service

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestCallCompleteCandidateKeepsCredentialsAndEffectsTransactional(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
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
	audits, settings, changes := count(&model.AuditEvent{}), count(&model.Setting{}), count(&model.Changes{})
	const cookie = "private-call-cookie-canary"
	const password = "private-call-password-canary"
	const joinLink = "https://dion.vc/event/private-call-room-canary"
	cookies, _ := json.Marshal([]map[string]string{{"name": "session", "value": cookie}})
	replace := func(value string) map[string]string { return map[string]string{"intent": "replace", "value": value} }
	draft := map[string]any{"type": "call", "tag": "owned-call", "platform": "private-invalid-platform", "read_buffer": 32768,
		"cookies": replace(string(cookies)), "password": replace(password), "join_link": replace(joinLink)}
	raw, _ := json.Marshal(draft)
	if _, err := svc.Save("inbounds", "new", raw, "", "admin", "fixture.invalid"); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_CANDIDATE_REJECTED") || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid Call candidate was not privately rejected", err)
	}
	if count(&model.Inbound{}) != 0 || count(&model.AuditEvent{}) != audits || count(&model.Setting{}) != settings || count(&model.Changes{}) != changes || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || svc.getLastUpdate() != 12345 {
		t.Fatal("failed Call candidate committed identity, secret, audit, Change or effects")
	}
	draft["platform"], draft["email"], draft["detour"] = "dion", "operator@fixture.invalid", "direct"
	raw, _ = json.Marshal(draft)
	if _, err := svc.Save("inbounds", "new", raw, "", "admin", "fixture.invalid"); err != nil {
		t.Fatal("valid private Call candidate rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil || singboxvalidation.ValidateConfig(*config) != nil || bytes.Contains(*config, []byte("sbox:v1:")) {
		t.Fatal("Call runtime configuration lost its private owner binding", err)
	}
	for _, secret := range []string{cookie, password, joinLink} {
		if !bytes.Contains(*config, []byte(secret)) {
			t.Fatal("trusted Call runtime did not receive its credential")
		}
	}
	var row model.Inbound
	if err := db.Where("tag = ?", "owned-call").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	read, err := entityinbounds.GetByIDs(db, strconv.Itoa(int(row.Id)))
	encoded, _ := json.Marshal(read)
	if err != nil || bytes.Contains(encoded, []byte("canary")) || bytes.Contains(encoded, []byte("sbox:v1:")) || bytes.Contains(row.Options, []byte("canary")) {
		t.Fatal("Call normal read or storage disclosed a private value")
	}
}
