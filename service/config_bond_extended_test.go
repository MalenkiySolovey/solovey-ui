//go:build shtorm_extended && with_quic

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestBondCompleteCandidateUsesResolvedMembersWithoutChangingStorage(t *testing.T) {
	for _, kind := range []string{"bond", "core-failover"} {
		t.Run(kind, func(t *testing.T) { testInboundGroupCompleteCandidate(t, kind) })
	}
}

func testInboundGroupCompleteCandidate(t *testing.T, kind string) {
	group := func(value string) json.RawMessage {
		return json.RawMessage(strings.Replace(value, `"type":"bond"`, `"type":"`+kind+`"`, 1))
	}
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	db := dbsqlite.DB()
	for _, row := range []model.Inbound{
		{Type: "socks", Tag: "first", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23464}`)},
		{Type: "socks", Tag: "second", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23465}`)},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc, _, _ := newHotReloadConfigService(t)
	if kind == "core-failover" {
		if _, err := svc.Save("inbounds", "new", json.RawMessage(`{"type":"failover","tag":"raw-alias","inbounds":["first","second"]}`), "", "admin", "fixture.invalid"); err == nil {
			t.Fatal("native alias bypassed canonical group ownership")
		}
	}
	if _, err := svc.Save("inbounds", "new", group(`{"type":"bond","tag":"group","inbounds":[{"type":"socks","tag":"copied"}]}`), "", "admin", "fixture.invalid"); err == nil {
		t.Fatal("editor accepted a copied runtime member as canonical storage")
	}
	if _, err := svc.Save("inbounds", "new", group(`{"type":"bond","tag":"group","inbounds":["first","second"]}`), "", "admin", "fixture.invalid"); err != nil {
		t.Fatal("valid resolved group candidate was rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if err := singboxvalidation.ValidateConfig(*config); err != nil {
		t.Fatal("resolved candidate fails the serving validator", err)
	}
	var stored model.Inbound
	var members struct {
		Inbounds []string `json:"inbounds"`
	}
	if err := db.Where("tag = ?", "group").First(&stored).Error; err != nil || json.Unmarshal(stored.Options, &members) != nil || len(members.Inbounds) != 2 || members.Inbounds[0] != "first" || members.Inbounds[1] != "second" {
		t.Fatal("runtime projection copied member options into canonical storage", err)
	}
}
