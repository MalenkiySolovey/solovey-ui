package outbounds

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFailoverDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Outbound{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAssembleFailoverForCore(t *testing.T) {
	outbound := model.Outbound{
		Type: FailoverType,
		Tag:  "group",
		Options: json.RawMessage(`{
			"outbounds":["primary","backup"],
			"interrupt_exist_connections":true,
			"failover":{"probe_target":"https://example.com/","interval":"30s","hysteresis":2}
		}`),
	}
	got, err := AssembleFailoverForCore(outbound, "direct")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "selector" || decoded["default"] != "primary" {
		t.Fatalf("assembled selector = %#v", decoded)
	}
	if _, leaked := decoded["failover"]; leaked {
		t.Fatal("panel-only failover settings leaked into sing-box config")
	}
	members, ok := decoded["outbounds"].([]any)
	if !ok || len(members) != 3 || members[2] != "direct" {
		t.Fatalf("members = %#v, want primary, backup, direct", decoded["outbounds"])
	}
}

func TestValidateFailoverGroup(t *testing.T) {
	db := newFailoverDB(t)
	rows := []model.Outbound{
		{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		{Type: "socks", Tag: "primary", Options: json.RawMessage(`{}`)},
		{Type: "socks", Tag: "backup", Options: json.RawMessage(`{}`)},
		{Type: "selector", Tag: "selector", Options: json.RawMessage(`{"outbounds":["primary"]}`)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	group := func(options string) model.Outbound {
		return model.Outbound{Type: FailoverType, Tag: "group", Options: json.RawMessage(options)}
	}
	if err := validateFailoverGroup(db, group(`{"outbounds":["primary","backup"]}`)); err != nil {
		t.Fatalf("valid group rejected: %v", err)
	}
	for name, options := range map[string]string{
		"empty":          `{"outbounds":[]}`,
		"missing":        `{"outbounds":["missing"]}`,
		"group member":   `{"outbounds":["selector"]}`,
		"self reference": `{"outbounds":["group"]}`,
		"duplicate":      `{"outbounds":["primary","primary"]}`,
		"bad scheme":     `{"outbounds":["primary"],"failover":{"probe_target":"ftp://example.com/"}}`,
		"metadata IP":    `{"outbounds":["primary"],"failover":{"probe_target":"http://169.254.169.254/"}}`,
		"tiny interval":  `{"outbounds":["primary"],"failover":{"interval":"1s"}}`,
		"bad hysteresis": `{"outbounds":["primary"],"failover":{"hysteresis":-1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateFailoverGroup(db, group(options)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestGetAllConfigAssemblesFailoverAndPicksDirectFallback(t *testing.T) {
	db := newFailoverDB(t)
	rows := []model.Outbound{
		{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		{Type: "socks", Tag: "primary", Options: json.RawMessage(`{"server":"127.0.0.1","server_port":1080}`)},
		{Type: FailoverType, Tag: "group", Options: json.RawMessage(`{"outbounds":["primary"]}`)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	configs, err := GetAllConfig(db)
	if err != nil {
		t.Fatal(err)
	}
	var group map[string]any
	if err := json.Unmarshal(configs[2], &group); err != nil {
		t.Fatal(err)
	}
	if group["type"] != "selector" {
		t.Fatalf("runtime type = %v, want selector", group["type"])
	}
}
