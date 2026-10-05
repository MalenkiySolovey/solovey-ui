package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
	opsdoctor "github.com/MalenkiySolovey/solovey-ui/internal/ops/doctor"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"gorm.io/gorm"
)

type capabilityCoreProbe struct {
	calls   []string
	configs [][]byte
}

func (p *capabilityCoreProbe) IsRunning() bool { return true }
func (p *capabilityCoreProbe) RemoveInbound(tag string) error {
	p.calls = append(p.calls, "remove-in:"+tag)
	return nil
}
func (p *capabilityCoreProbe) RemoveOutbound(tag string) error {
	p.calls = append(p.calls, "remove-out:"+tag)
	return nil
}
func (p *capabilityCoreProbe) RemoveEndpoint(tag string) error {
	p.calls = append(p.calls, "remove-endpoint:"+tag)
	return nil
}
func (p *capabilityCoreProbe) RemoveService(tag string) error {
	p.calls = append(p.calls, "remove-service:"+tag)
	return nil
}
func (p *capabilityCoreProbe) CloseInboundConnections(string) {}
func (p *capabilityCoreProbe) add(config []byte) error {
	p.calls = append(p.calls, "add")
	p.configs = append(p.configs, append([]byte(nil), config...))
	return nil
}
func (p *capabilityCoreProbe) AddInbound(config []byte) error  { return p.add(config) }
func (p *capabilityCoreProbe) AddOutbound(config []byte) error { return p.add(config) }
func (p *capabilityCoreProbe) AddEndpoint(config []byte) error { return p.add(config) }
func (p *capabilityCoreProbe) AddService(config []byte) error  { return p.add(config) }

func TestCapabilityConsumersAgreeForEveryAuthoritativeEntityFact(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	if err := db.Create(&model.Outbound{Type: "direct", Tag: "capability-seed", Options: json.RawMessage("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	for _, fact := range entitycapabilities.Current().Facts {
		tag := "capability-" + fact.Category + "-" + fact.Type
		options := json.RawMessage("{\"type\":\"forged-unavailable\",\"tag\":\"forged-tag\"}")
		if fact.Type == "failover" {
			options = json.RawMessage("{\"outbounds\":[\"capability-seed\"],\"final\":\"reject\"}")
		}
		var row any
		switch fact.Category {
		case "inbounds":
			row = &model.Inbound{Type: fact.Type, Tag: tag, Options: options}
		case "outbounds":
			row = &model.Outbound{Type: fact.Type, Tag: tag, Options: options}
		case "endpoints":
			row = &model.Endpoint{Type: fact.Type, Tag: tag, Options: options}
		case "services":
			row = &model.Service{Type: fact.Type, Tag: tag, Options: options}
		default:
			continue
		}
		t.Run(fact.Category+"/"+fact.Type, func(t *testing.T) {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			var configs []json.RawMessage
			var err error
			switch fact.Category {
			case "inbounds":
				configs, err = entityinbounds.GetAllConfig(db, nil)
			case "outbounds":
				configs, err = entityoutbounds.GetAllConfig(db)
			case "endpoints":
				configs, err = entityendpoints.GetAllConfig(db)
			case "services":
				configs, err = entityservices.GetAllConfig(db)
			}
			if err != nil {
				t.Fatal(err)
			}
			var generated bool
			for _, config := range configs {
				var identity struct{ Type, Tag string }
				if err := json.Unmarshal(config, &identity); err != nil {
					t.Fatal(err)
				}
				if identity.Tag == tag {
					generated = true
					if identity.Type != fact.RuntimeType {
						t.Fatalf("runtime identity disagrees: %s", config)
					}
				}
			}
			if generated != fact.Available {
				t.Fatalf("generator=%v fact=%+v", generated, fact)
			}
			if accepted := saveeligibility.Check(fact.Category, fact.Type, "new", "") == nil; accepted != fact.Available {
				t.Fatalf("save=%v runtime=%v", accepted, generated)
			}
			entities, err := runtimeprojection.Stored(db)
			if err != nil {
				t.Fatal(err)
			}
			var stored runtimeprojection.Entity
			for _, entity := range entities {
				if entity.Category == fact.Category && entity.Tag == tag {
					stored = entity
					break
				}
			}
			if stored.ID == 0 {
				t.Fatal("stored projection missed row")
			}
			diagnosed := len(opsdoctor.CapabilityFindings([]runtimeprojection.Entity{stored})) > 0
			if diagnosed == fact.Available {
				t.Fatalf("Doctor=%v fact=%+v", diagnosed, fact)
			}
			probe := &capabilityCoreProbe{}
			switch fact.Category {
			case "inbounds":
				err = entityinbounds.Restart(db, []uint{stored.ID}, probe, nil)
			case "outbounds":
				err = entityoutbounds.Restart(db, []uint{stored.ID}, probe)
			case "endpoints":
				err = entityendpoints.Restart(db, []uint{stored.ID}, probe)
			case "services":
				err = entityservices.Restart(db, []uint{stored.ID}, probe)
			}
			if err != nil {
				t.Fatal(err)
			}
			var reloaded bool
			for _, config := range probe.configs {
				var identity struct{ Type, Tag string }
				if err := json.Unmarshal(config, &identity); err != nil {
					t.Fatal(err)
				}
				if identity.Tag == tag {
					reloaded = true
					if identity.Type != fact.RuntimeType {
						t.Fatalf("reload identity disagrees: %s", config)
					}
				}
			}
			if reloaded != generated {
				t.Fatalf("full=%v reload=%v", generated, reloaded)
			}
			if !fact.Available && len(probe.calls) != 0 {
				t.Fatal("unavailable row mutated live runtime")
			}
		})
	}
}

func TestCapabilitySaveRejectsNewChangeAndForgedIdentityBeforeWrites(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	service := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	for _, category := range []string{"inbounds", "outbounds", "endpoints", "services"} {
		_, err := service.Save(category, "new", json.RawMessage("{\"type\":\"unrecognized\",\"tag\":\"rejected\"}"), "", "admin", "example.invalid")
		var rejected *saveeligibility.Rejection
		if !errors.As(err, &rejected) || rejected.ReasonCode() != "UNKNOWN_ENTITY_TYPE" {
			t.Fatalf("%s: %v", category, err)
		}
	}
	known := entitycapabilities.Resolve("endpoints", "tailscale")
	if known.Available {
		t.Skip("this binary includes Tailscale; unavailable cases are qualified in untagged/minimal builds")
	}
	original := model.Endpoint{Type: "wireguard", Tag: "original", Options: json.RawMessage("{}")}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ action, payload string }{
		{"new", "{\"id\":0,\"type\":\"tailscale\",\"tag\":\"new-unavailable\"}"},
		{"edit", fmt.Sprintf("{\"id\":%d,\"type\":\"tailscale\",\"oldType\":\"tailscale\",\"tag\":\"original\"}", original.Id)},
	} {
		_, err := service.Save("endpoints", test.action, json.RawMessage(test.payload), "", "admin", "example.invalid")
		var rejected *saveeligibility.Rejection
		if !errors.As(err, &rejected) || rejected.ReasonCode() != known.Reason {
			t.Fatalf("unavailable %s: %v", test.action, err)
		}
	}
	_, err := service.Save("endpoints", "new", json.RawMessage(fmt.Sprintf("{\"id\":%d,\"type\":\"wireguard\",\"tag\":\"forged\"}", original.Id)), "", "admin", "example.invalid")
	if err == nil {
		t.Fatal("forged create id accepted")
	}
	var saved model.Endpoint
	if err := db.First(&saved, original.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Type != "wireguard" || saved.Tag != "original" {
		t.Fatal("rejected change wrote row")
	}
	historical := model.Endpoint{Type: "tailscale", Tag: "historical", Options: json.RawMessage("{}")}
	if err := db.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save("endpoints", "edit", json.RawMessage(fmt.Sprintf("{\"id\":%d,\"type\":\"tailscale\",\"tag\":\"historical\",\"hostname\":\"edited\"}", historical.Id)), "", "admin", "example.invalid"); err != nil {
		t.Fatalf("safe historical edit: %v", err)
	}
	var count int64
	if err := db.Model(&model.Endpoint{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("partial writes: %d", count)
	}
}

type capabilityWarpHook struct{ calls int }

func (h *capabilityWarpHook) RegisterWarp(*model.Endpoint) error           { h.calls++; return nil }
func (h *capabilityWarpHook) SetWarpLicense(string, *model.Endpoint) error { h.calls++; return nil }

func TestCapabilityWarpSaveFinalFullReloadAndDoctorAgree(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	hook := &capabilityWarpHook{}
	payload := json.RawMessage(fmt.Sprintf("{\"type\":\"warp\",\"tag\":\"semantic-final\",\"system\":false,\"address\":[\"10.0.0.2/32\"],\"private_key\":%q,\"peers\":[],\"mtu\":1408}", hotReloadWireguardKey))
	var changeIDs []uint
	if err := db.Transaction(func(tx *gorm.DB) error {
		change, err := entityendpoints.Save(tx, "new", payload, hook)
		if err == nil {
			changeIDs = change.ReloadIDs
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if hook.calls != 1 || len(changeIDs) != 1 {
		t.Fatal("WARP owner not reached exactly once")
	}
	base := "{\"dns\":{\"servers\":[]},\"route\":{\"rules\":[],\"final\":\"semantic-final\"},\"experimental\":{}}"
	if err := db.Create(&model.Setting{Key: "config", Value: base}).Error; err != nil {
		t.Fatal(err)
	}
	projection, err := NewSingBoxConfigBuilder(NewRuntimeWithCoreProvider(nil)).BuildProjectionFromDB(db, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Eligibility.Omitted) != 0 {
		t.Fatalf("supported WARP omitted: %+v", projection.Eligibility)
	}
	var config struct {
		Endpoints []map[string]any
		Route     map[string]any
	}
	if err := json.Unmarshal(projection.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.Route["final"] != "semantic-final" || len(config.Endpoints) != 1 || config.Endpoints[0]["type"] != "wireguard" || config.Endpoints[0]["tag"] != "semantic-final" {
		t.Fatal("WARP full projection lost identity")
	}
	assertCapabilityWarpDryCheck(t, singboxvalidation.ValidateConfig(projection.Config))
	probe := &capabilityCoreProbe{}
	if err := entityendpoints.Restart(db, changeIDs, probe); err == nil {
		t.Fatal("captured default endpoint hot replacement accepted")
	}
	if len(probe.calls) != 0 {
		t.Fatal("default endpoint rejection mutated core")
	}
	var original map[string]any
	if err := json.Unmarshal(payload, &original); err != nil {
		t.Fatal(err)
	}
	original["id"] = changeIDs[0]
	edited, _ := json.Marshal(original)
	if err := db.Transaction(func(tx *gorm.DB) error {
		change, err := entityendpoints.Save(tx, "edit", edited, hook)
		if err == nil && !change.NeedsRestart {
			t.Fatal("default endpoint edit did not select full restart")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	original["id"] = 0
	original["tag"] = "semantic-hot"
	hot, _ := json.Marshal(original)
	var hotIDs []uint
	if err := db.Transaction(func(tx *gorm.DB) error {
		change, err := entityendpoints.Save(tx, "new", hot, hook)
		if err == nil {
			hotIDs = change.ReloadIDs
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := entityendpoints.Restart(db, hotIDs, probe); err != nil {
		t.Fatal(err)
	}
	var identity struct{ Type, Tag string }
	if len(probe.configs) != 1 {
		t.Fatal("reload missed WARP")
	}
	if err := json.Unmarshal(probe.configs[0], &identity); err != nil {
		t.Fatal(err)
	}
	if identity.Type != "wireguard" || identity.Tag != "semantic-hot" {
		t.Fatal("reload disagrees with full projection")
	}
	if findings := opsdoctor.CapabilityChecks(db); len(findings) != 0 {
		t.Fatalf("false WARP unsupported: %+v", findings)
	}
	var stored model.Endpoint
	if err := db.First(&stored, changeIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Type != "warp" {
		t.Fatal("storage alias rewritten")
	}
}

func TestCapabilityOmissionReferencesRejectBeforeCommitAndLiveRemoval(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	builder := NewSingBoxConfigBuilder(NewRuntimeWithCoreProvider(nil))
	unavailable := model.Outbound{Type: "historical-unknown", Tag: "unavailable", Options: json.RawMessage("{}")}
	if err := db.Create(&unavailable).Error; err != nil {
		t.Fatal(err)
	}
	projection, err := builder.BuildProjectionFromDB(db, "{}")
	if err != nil || len(projection.Eligibility.Omitted) != 1 {
		t.Fatalf("unused historical: %+v %v", projection.Eligibility, err)
	}
	for _, base := range []string{
		"{\"route\":{\"final\":\"unavailable\"}}",
		"{\"dns\":{\"servers\":[{\"type\":\"https\",\"tag\":\"dns\",\"detour\":\"unavailable\"}]}}",
		"{\"ntp\":{\"detour\":\"unavailable\"}}",
	} {
		if _, err := builder.BuildProjectionFromDB(db, base); err == nil {
			t.Fatal("candidate reference to omitted row accepted")
		}
	}
	service := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	_, err = service.Save("config", "edit", json.RawMessage("{\"route\":{\"final\":\"unavailable\"}}"), "", "admin", "example.invalid")
	var rejected *runtimeprojection.Rejection
	if !errors.As(err, &rejected) {
		t.Fatalf("base save rejection: %v", err)
	}
	var count int64
	if err := db.Model(&model.Setting{}).Where("key = ?", "config").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rejected base committed")
	}
	group := model.Outbound{Type: "failover", Tag: "group", Options: json.RawMessage("{\"outbounds\":[\"unavailable\"],\"final\":\"reject\"}")}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	probe := &capabilityCoreProbe{}
	if err := entityoutbounds.Restart(db, []uint{group.Id}, probe); err == nil {
		t.Fatal("dangling failover hot reload accepted")
	}
	if len(probe.calls) != 0 {
		t.Fatal("rejected projection mutated live core")
	}
}
