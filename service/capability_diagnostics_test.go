package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"gorm.io/gorm"
)

func TestCapabilityDoctorSurvivesMalformedBaseAndReportsReferenceDetails(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	unknown := model.Outbound{Type: "unknown-history", Tag: "historical", Options: json.RawMessage("{")}
	if err := db.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	setting := model.Setting{Key: "config", Value: "{"}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	doctor := &DoctorService{Runtime: NewRuntimeWithCoreProvider(nil)}
	report := doctor.Run("example.invalid")
	found := false
	for _, item := range report.Items {
		if entity, ok := item.Details.(runtimeprojection.Entity); ok && entity.ID == unknown.Id {
			found = true
			if entity.Capability.Reason != "UNKNOWN_ENTITY_TYPE" {
				t.Fatal("unknown history lost reason")
			}
		}
	}
	if !found {
		t.Fatal("malformed base suppressed capability diagnostics")
	}
	if err := db.Model(&setting).Update("value", `{"route":{"final":"historical"}}`).Error; err != nil {
		t.Fatal(err)
	}
	report = doctor.Run("example.invalid")
	found = false
	for _, item := range report.Items {
		if projection, ok := item.Details.(runtimeprojection.Report); ok && len(projection.References) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("Doctor lost typed unavailable dependency report")
	}
}

func TestCapabilityCapturedTailscaleConsumersChooseFullRestart(t *testing.T) {
	if !entitycapabilities.Resolve("endpoints", "tailscale").Available {
		t.Skip("captured Tailscale adapters are exercised in the supported tagged profile")
	}
	for _, consumer := range []string{"dns", "derp"} {
		t.Run(consumer, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			endpoint := model.Endpoint{Type: "tailscale", Tag: "captured", Options: json.RawMessage("{}")}
			if err := db.Create(&endpoint).Error; err != nil {
				t.Fatal(err)
			}
			if consumer == "dns" {
				if err := db.Create(&model.Setting{Key: "config", Value: `{"dns":{"servers":[{"type":"tailscale","tag":"dns","endpoint":"captured"}]}}`}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if !entitycapabilities.Resolve("services", "derp").Available {
					t.Fatal("accepted tagged Tailscale profile must include its DERP contribution")
				}
				if err := db.Create(&model.Service{Type: "derp", Tag: "derp", Options: json.RawMessage(`{"verify_client_endpoint":["captured"]}`)}).Error; err != nil {
					t.Fatal(err)
				}
			}
			payload, _ := json.Marshal(map[string]any{"id": endpoint.Id, "type": "tailscale", "tag": "captured", "hostname": "updated"})
			if err := db.Transaction(func(tx *gorm.DB) error {
				change, err := entityendpoints.Save(tx, "edit", payload, nil)
				if err == nil && !change.NeedsRestart {
					t.Fatal("captured endpoint chose partial replacement")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			probe := &capabilityCoreProbe{}
			if err := entityendpoints.Restart(db, []uint{endpoint.Id}, probe); err == nil {
				t.Fatal("captured adapter accepted direct replacement")
			}
			if len(probe.calls) != 0 {
				t.Fatal("captured reload rejection mutated live adapter")
			}
		})
	}
}
