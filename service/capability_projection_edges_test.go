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
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
)

func TestCapabilityReloadPreflightsEntireBatchBeforeRemoval(t *testing.T) {
	for _, category := range []string{"inbounds", "outbounds", "endpoints", "services"} {
		t.Run(category, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			var first, second any
			switch category {
			case "inbounds":
				first = &model.Inbound{Type: "mixed", Tag: "first", Options: json.RawMessage("{}")}
				second = &model.Inbound{Type: "mixed", Tag: "second", Options: json.RawMessage("{")}
			case "outbounds":
				first = &model.Outbound{Type: "direct", Tag: "first", Options: json.RawMessage("{}")}
				second = &model.Outbound{Type: "direct", Tag: "second", Options: json.RawMessage("{")}
			case "endpoints":
				first = &model.Endpoint{Type: "wireguard", Tag: "first", Options: json.RawMessage("{}")}
				second = &model.Endpoint{Type: "wireguard", Tag: "second", Options: json.RawMessage("{")}
			case "services":
				first = &model.Service{Type: "ssm-api", Tag: "first", Options: json.RawMessage("{}")}
				second = &model.Service{Type: "ssm-api", Tag: "second", Options: json.RawMessage("{")}
			}
			if err := db.Create(first).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(second).Error; err != nil {
				t.Fatal(err)
			}
			stored, err := runtimeprojection.Stored(db)
			if err != nil {
				t.Fatal(err)
			}
			var ids []uint
			for _, row := range stored {
				if row.Category == category {
					ids = append(ids, row.ID)
				}
			}
			probe := &capabilityCoreProbe{}
			switch category {
			case "inbounds":
				err = entityinbounds.Restart(db, ids, probe, nil)
			case "outbounds":
				err = entityoutbounds.Restart(db, ids, probe)
			case "endpoints":
				err = entityendpoints.Restart(db, ids, probe)
			case "services":
				err = entityservices.Restart(db, ids, probe)
			}
			if err == nil {
				t.Fatal("malformed second row accepted")
			}
			if len(probe.calls) != 0 {
				t.Fatalf("preflight failure mutated first adapter: %v", probe.calls)
			}
		})
	}
}

func TestCapabilityOmissionDependenciesIncludeClientServiceAndNestedRules(t *testing.T) {
	for _, example := range []string{"failover-final", "ssm-inbound", "inbound-detour", "client-inbound", "nested-inbound-rule", "dns-endpoint", "dns-service", "derp-endpoint"} {
		t.Run(example, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			base := []byte("{}")
			insert := func(value any) {
				t.Helper()
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			switch example {
			case "failover-final":
				insert(&model.Outbound{Type: "unknown", Tag: "missing", Options: json.RawMessage("{}")})
				insert(&model.Outbound{Type: "direct", Tag: "member", Options: json.RawMessage("{}")})
				insert(&model.Outbound{Type: "failover", Tag: "group", Options: json.RawMessage(`{"outbounds":["member"],"final":"missing"}`)})
			case "dns-endpoint", "derp-endpoint":
				insert(&model.Endpoint{Type: "unknown", Tag: "missing", Options: json.RawMessage("{}")})
				if example == "dns-endpoint" {
					base = []byte(`{"dns":{"servers":[{"type":"tailscale","tag":"dns","endpoint":"missing"}]}}`)
				} else {
					if !entitycapabilities.Resolve("services", "derp").Available {
						t.Skip("DERP reference is exercised in the supported Tailscale/gVisor profile")
					}
					insert(&model.Service{Type: "derp", Tag: "service", Options: json.RawMessage(`{"verify_client_endpoint":["missing"]}`)})
				}
			case "dns-service":
				insert(&model.Service{Type: "unknown", Tag: "missing", Options: json.RawMessage("{}")})
				base = []byte(`{"dns":{"servers":[{"type":"resolved","tag":"dns","service":"missing"}]}}`)
			default:
				missing := &model.Inbound{Type: "unknown", Tag: "missing", Options: json.RawMessage("{}")}
				insert(missing)
				switch example {
				case "ssm-inbound":
					insert(&model.Service{Type: "ssm-api", Tag: "service", Options: json.RawMessage(`{"servers":{"/main":"missing"}}`)})
				case "inbound-detour":
					insert(&model.Inbound{Type: "mixed", Tag: "included", Options: json.RawMessage(`{"detour":"missing"}`)})
				case "client-inbound":
					insert(&model.Client{Name: "client", Inbounds: json.RawMessage(fmt.Sprintf("[%d]", missing.Id)), Config: json.RawMessage("{}")})
				case "nested-inbound-rule":
					base = []byte(`{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"inbound":["missing"]}]}]}}`)
				}
			}
			report, err := runtimeprojection.ValidateReferences(db, base)
			var rejected *runtimeprojection.Rejection
			if !errors.As(err, &rejected) || len(report.References) == 0 {
				t.Fatalf("dependency escaped projection: %+v %v", report, err)
			}
		})
	}
}

func TestCapabilityReferencedSavePreservesCommitAndLiveRuntime(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	if err := db.Create(&model.Outbound{Type: "unknown", Tag: "missing", Options: json.RawMessage("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	service, applier, lifecycle := newHotReloadConfigService(t)
	_, err := service.Save("outbounds", "new", json.RawMessage(`{"type":"socks","tag":"candidate","server":"127.0.0.1","server_port":1234,"detour":"missing"}`), "", "admin", "example.invalid")
	var rejected *runtimeprojection.Rejection
	if !errors.As(err, &rejected) {
		t.Fatalf("expected typed reference rejection: %v", err)
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "candidate").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(applier.calls) != 0 || len(lifecycle.calls) != 0 || !service.IsCoreRunning() {
		t.Fatal("rejected candidate committed or changed the running core")
	}
}

func TestCapabilityDefaultEndpointSelectsFullLifecycleForEitherIdentity(t *testing.T) {
	for _, panelType := range []string{"wireguard", "warp"} {
		t.Run(panelType, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			endpoint := createHotReloadEndpoint(t, "default-endpoint")
			if err := db.Model(&endpoint).Update("type", panelType).Error; err != nil {
				t.Fatal(err)
			}
			seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"route":{"final":"default-endpoint"}}`))
			service, applier, lifecycle := newHotReloadConfigService(t)
			var payload map[string]any
			if err := json.Unmarshal(endpointHotReloadPayload(endpoint.Id, endpoint.Tag, 1412), &payload); err != nil {
				t.Fatal(err)
			}
			payload["type"] = panelType
			payload["ext"] = map[string]string{"license_key": ""}
			encoded, _ := json.Marshal(payload)
			// Empty unchanged optional license is an owner-local no-op, so the WARP
			// facade is exercised without external registration or credential changes.
			if _, err := service.Save("endpoints", "edit", encoded, "", "admin", "example.invalid"); err != nil {
				t.Fatal(err)
			}
			var stored model.Endpoint
			if err := db.First(&stored, endpoint.Id).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Type != panelType {
				t.Fatal("stored semantic identity changed")
			}
			if len(applier.calls) != 0 || len(lifecycle.calls) != 1 || lifecycle.calls[0] != "restart" {
				t.Fatalf("default endpoint used partial replacement: %v %v", applier.calls, lifecycle.calls)
			}
		})
	}
}
