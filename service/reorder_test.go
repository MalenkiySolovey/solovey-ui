package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	netentitysvc "github.com/MalenkiySolovey/solovey-ui/service/netentity"
)

func TestReorderOutboundsPreservesImplicitRouteFinal(t *testing.T) {
	settingService := initSettingTestDB(t)
	db := dbsqlite.DB()

	if err := saveTestBaseConfig(settingService, `{"dns":{"servers":[],"rules":[]},"route":{"rules":[]},"experimental":{}}`); err != nil {
		t.Fatal(err)
	}

	var direct model.Outbound
	if err := db.Where("tag = ?", "direct").First(&direct).Error; err != nil {
		t.Fatal(err)
	}
	direct.SortOrder = 1
	if err := db.Save(&direct).Error; err != nil {
		t.Fatal(err)
	}
	proxy := model.Outbound{SortOrder: 2, Type: "direct", Tag: "proxy", Options: json.RawMessage(`{}`)}
	if err := db.Create(&proxy).Error; err != nil {
		t.Fatal(err)
	}

	order := json.RawMessage(fmt.Sprintf("[%d,%d]", proxy.Id, direct.Id))
	if _, err := (&ConfigService{}).Reorder("outbounds", order, "admin"); err != nil {
		t.Fatal(err)
	}

	var rows []model.Outbound
	if err := db.Model(model.Outbound{}).Order(entityorder.Clause).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	gotTags := []string{rows[0].Tag, rows[1].Tag}
	if !reflect.DeepEqual(gotTags, []string{"proxy", "direct"}) {
		t.Fatalf("outbound order = %v, want proxy,direct", gotTags)
	}

	config := mustTestConfigMap(t, settingService)
	route := config["route"].(map[string]any)
	if route["final"] != "direct" {
		t.Fatalf("route.final = %#v, want old implicit first outbound direct", route["final"])
	}
}

func TestReorderDNSServersPreservesImplicitFinal(t *testing.T) {
	settingService := initSettingTestDB(t)
	if err := saveTestBaseConfig(settingService, `{
		"dns":{"servers":[{"tag":"local","address":"local"},{"tag":"remote","address":"tls://1.1.1.1"}],"rules":[]},
		"route":{"rules":[]},
		"experimental":{}
	}`); err != nil {
		t.Fatal(err)
	}

	if _, err := (&ConfigService{}).Reorder("dnsServers", json.RawMessage(`["remote","local"]`), "admin"); err != nil {
		t.Fatal(err)
	}

	config := mustTestConfigMap(t, settingService)
	dns := config["dns"].(map[string]any)
	if dns["final"] != "local" {
		t.Fatalf("dns.final = %#v, want old implicit first server local", dns["final"])
	}
	servers := dns["servers"].([]any)
	if servers[0].(map[string]any)["tag"] != "remote" || servers[1].(map[string]any)["tag"] != "local" {
		t.Fatalf("dns.servers order = %#v, want remote,local", servers)
	}
}

func TestReorderRejectsPartialIDList(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	proxy := model.Outbound{SortOrder: 2, Type: "direct", Tag: "proxy", Options: json.RawMessage(`{}`)}
	if err := db.Create(&proxy).Error; err != nil {
		t.Fatal(err)
	}

	var direct model.Outbound
	if err := db.Where("tag = ?", "direct").First(&direct).Error; err != nil {
		t.Fatal(err)
	}
	_, err := (&ConfigService{}).Reorder("outbounds", json.RawMessage(fmt.Sprintf("[%d]", direct.Id)), "admin")
	if err == nil {
		t.Fatal("partial reorder list should be rejected")
	}
}

func TestReorderClientsControlsGeneratedInboundUserOrder(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()

	inbound := model.Inbound{SortOrder: 1, Type: "vmess", Tag: "vmess-in", Options: json.RawMessage(`{}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	alice := model.Client{
		SortOrder: 1,
		Enable:    true,
		Name:      "alice",
		Config:    json.RawMessage(`{"vmess":{"name":"alice"}}`),
		Inbounds:  json.RawMessage(fmt.Sprintf("[%d]", inbound.Id)),
	}
	bob := model.Client{
		SortOrder: 2,
		Enable:    true,
		Name:      "bob",
		Config:    json.RawMessage(`{"vmess":{"name":"bob"}}`),
		Inbounds:  json.RawMessage(fmt.Sprintf("[%d]", inbound.Id)),
	}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatal(err)
	}

	order := json.RawMessage(fmt.Sprintf("[%d,%d]", bob.Id, alice.Id))
	if _, err := (&ConfigService{}).Reorder("clients", order, "admin"); err != nil {
		t.Fatal(err)
	}

	inboundJSON, err := json.Marshal(map[string]any{"type": "vmess", "tag": "vmess-in"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := (&netentitysvc.InboundService{}).AddUsers(db, inboundJSON, inbound.Id, inbound.Type)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	names := []string{got.Users[0]["name"].(string), got.Users[1]["name"].(string)}
	if !reflect.DeepEqual(names, []string{"bob", "alice"}) {
		t.Fatalf("generated users order = %v, want bob,alice", names)
	}
}

func saveTestBaseConfig(settingService *SettingService, raw string) error {
	tx := dbsqlite.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := settingService.SaveConfig(tx, json.RawMessage(raw)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func mustTestConfigMap(t *testing.T, settingService *SettingService) map[string]any {
	t.Helper()
	raw, err := settingService.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	return config
}
