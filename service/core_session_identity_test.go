package service

import (
	"encoding/json"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestCoreCandidateCapturesRenderedCredentialBindings(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Inbound{Id: 1, Type: "socks", Tag: "identity-in", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":0}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Client{Id: 7, Name: "display label", Enable: true, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"socks":{"username":"authenticated","password":"fixture-only"}}`)}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewConfigServiceWithRuntime(NewRuntime(coreruntime.NewCore()))
	config, bindings, err := s.coreCandidate()
	if err != nil {
		t.Fatal(err)
	}
	var candidate struct {
		Inbounds []struct {
			Tag   string
			Users []struct{ Username string }
		}
	}
	if json.Unmarshal(config, &candidate) != nil || len(candidate.Inbounds) != 1 || candidate.Inbounds[0].Users[0].Username != "authenticated" {
		t.Fatal("wrong rendered authenticated principal")
	}
	if len(bindings) != 1 || bindings[0].ClientID != 7 || bindings[0].Principal != "authenticated" || bindings[0].Inbound != "identity-in" {
		t.Fatal("desired-state binding not injected with candidate")
	}
	if err := db.Model(&model.Client{}).Where("id = ?", 7).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	_, bindings, err = s.coreCandidate()
	if err != nil || len(bindings) != 0 {
		t.Fatal("disabled membership remained in candidate identity")
	}
}
