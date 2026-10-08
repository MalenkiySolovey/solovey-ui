package sessionidentity

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func identityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestCaptureUsesCredentialMembershipAndStableID(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Inbound{Id: 1, Tag: "in", Type: "anytls"}).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Id: 7, Enable: true, Name: "different display label", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"anytls":{"name":"authenticated-name","password":"fixture-only"}}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"inbounds":[{"type":"anytls","tag":"in","users":[{"name":"authenticated-name","password":"fixture-only"}]}]}`)
	bindings, err := Capture(db, config)
	if err != nil || len(bindings) != 1 || bindings[0].ClientID != 7 {
		t.Fatal("accepted credential did not bind stable client ID")
	}
	for _, wrong := range []string{
		`{"type":"anytls","tag":"in","users":[{"name":"authenticated-name","password":"different"}]}`,
		`{"type":"anytls","tag":"other","users":[{"name":"authenticated-name","password":"fixture-only"}]}`,
		`{"type":"anytls","tag":"in","users":[{"name":"different display label","password":"fixture-only"}]}`,
	} {
		bindings, err := Capture(db, []byte(wrong))
		if err != nil || len(bindings) != 0 {
			t.Fatal("unproven client identity associated")
		}
	}
	if err := db.Model(&model.Client{}).Where("id = ?", 7).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	bindings, err = Capture(db, config)
	if err != nil || len(bindings) != 0 {
		t.Fatal("disabled client newly bound")
	}
}

func TestCaptureAmbiguousAndUnavailableRemainUnassociated(t *testing.T) {
	db := identityDB(t)
	if err := db.Create(&model.Inbound{Id: 1, Tag: "in", Type: "anytls"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint{1, 2} {
		if err := db.Create(&model.Client{Id: id, Enable: true, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"anytls":{"name":"alice","password":"fixture-only"}}`)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings, err := Capture(db, []byte(`{"type":"anytls","tag":"in","users":[{"name":"alice","password":"fixture-only"}]}`))
	if err != nil || len(bindings) != 0 {
		t.Fatal("ambiguous client guessed")
	}
	if _, err := Capture(nil, []byte(`{"type":"anytls","tag":"in","users":[{"name":"alice"}]}`)); err == nil {
		t.Fatal("unavailable catalogue accepted")
	}
}

func TestCaptureBasicAuthAndVisionRendererVariants(t *testing.T) {
	for _, kind := range []string{"socks", "vless"} {
		t.Run(kind, func(t *testing.T) {
			db := identityDB(t)
			if err := db.Create(&model.Inbound{Id: 1, Tag: "in", Type: kind}).Error; err != nil {
				t.Fatal(err)
			}
			credentials := `{"username":"authenticated","password":"fixture-only"}`
			actual := credentials
			if kind == "vless" {
				credentials = `{"name":"authenticated","uuid":"00000000-0000-4000-8000-000000000001","flow":"xtls-rprx-vision"}`
				actual = `{"name":"authenticated","uuid":"00000000-0000-4000-8000-000000000001","flow":""}`
			}
			if err := db.Create(&model.Client{Id: 7, Enable: true, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"` + kind + `":` + credentials + `}`)}).Error; err != nil {
				t.Fatal(err)
			}
			bindings, err := Capture(db, []byte(`{"type":"`+kind+`","tag":"in","users":[`+actual+`]}`))
			if err != nil || len(bindings) != 1 || bindings[0].ClientID != 7 {
				t.Fatal("supported renderer identity variant lost")
			}
		})
	}
}
