package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
)

func TestCapabilityWireGuardHistoryFollowsCurrentProvider(t *testing.T) {
	for _, field := range []string{"amnezia", "preallocated_buffers_per_pool", "disable_pauses"} {
		t.Run(field, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			value := map[string]any{"amnezia": map[string]any{"h1": 123, "h2": "200-300"}, "preallocated_buffers_per_pool": 4, "disable_pauses": false}[field]
			options, _ := json.Marshal(map[string]any{field: value, "future": json.RawMessage(`{"counter":9007199254740993}`)})
			row := model.Endpoint{Type: "wireguard", Tag: "preserved-awg", Options: options}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			available := registry.CriticalProtocolFields()["wireguard/endpoint"][field].Available
			rows, err := runtimeprojection.Stored(db)
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, resource := range rows {
				if resource.Category == "endpoints" && resource.ID == row.Id {
					matched = true
					if resource.Capability.Available != available {
						t.Fatal("endpoint eligibility does not follow actual SDK field", resource)
					}
				}
			}
			if !matched {
				t.Fatal("preserved endpoint missing from eligibility")
			}
			err = saveeligibility.Validate(db, "edit", row.Id, &model.Endpoint{}, "endpoints", "wireguard")
			if available {
				if err != nil {
					t.Fatal("supporting provider did not reactivate retained endpoint", err)
				}
			} else {
				var rejected *saveeligibility.Rejection
				if !errors.As(err, &rejected) || rejected.ReasonCode() != "PROTOCOL_FIELD_CAPABILITY_UNAVAILABLE" {
					t.Fatal("unsupported endpoint admitted ordinary editing", err)
				}
				config, err := entityendpoints.GetAllConfig(db)
				if err != nil || len(config) != 0 {
					t.Fatal("unsupported endpoint entered runtime", err)
				}
				projection, err := NewSingBoxConfigBuilder(NewRuntimeWithCoreProvider(nil)).BuildProjectionFromDB(db, "{}")
				if err != nil || len(projection.Eligibility.Omitted) != 1 {
					t.Fatal("inactive endpoint disappeared from runtime explanation", err)
				}
			}
			var preserved model.Endpoint
			if db.First(&preserved, row.Id).Error != nil || !bytes.Equal(preserved.Options, options) {
				t.Fatal("provider eligibility changed preserved desired state")
			}
		})
	}
}

// The same fixture runs in both immutable provider graphs: unsupported state
// is omitted and cannot be edited; supporting state becomes eligible again.
func TestCapabilityTransportHistoryFollowsCurrentProvider(t *testing.T) {
	for _, transport := range []string{"xhttp", "mkcp", "realm"} {
		t.Run(transport, func(t *testing.T) {
			initSettingTestDB(t)
			db := dbsqlite.DB()
			options := json.RawMessage(fmt.Sprintf("{ \"transport\":{\"type\":%q}, \"future\":{\"counter\":9007199254740993} }", transport))
			kind, reason := "vless", "TRANSPORT_CAPABILITY_UNAVAILABLE"
			available := registry.ResolveTransport(transport).Available()
			if transport == "realm" {
				kind, reason = "hysteria2", "PROTOCOL_FIELD_CAPABILITY_UNAVAILABLE"
				options = json.RawMessage(`{ "realm":{"server_url":"https://realm.invalid","realm_id":"preserved","token":"historical-private-token"}, "future":{"counter":9007199254740993} }`)
				available = registry.Resolve("outbounds", "hysteria2").Available() && registry.CriticalProtocolFields()["hysteria2/out"]["realm"].Available
			}
			in := model.Inbound{Type: kind, Tag: "preserved-in", Options: options, Addrs: json.RawMessage("[]"), OutJson: json.RawMessage(`{"future":true}`)}
			out := model.Outbound{Type: kind, Tag: "preserved-out", Options: options}
			if err := db.Create(&in).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&out).Error; err != nil {
				t.Fatal(err)
			}
			assignment := json.RawMessage(fmt.Sprintf("[%d]", in.Id))
			client := model.Client{Name: "preserved-client", Inbounds: assignment, Config: json.RawMessage(`{"future":true}`)}
			if err := db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			rows, err := runtimeprojection.Stored(db)
			if err != nil {
				t.Fatal(err)
			}
			matched := 0
			for _, row := range rows {
				if row.Tag != in.Tag && row.Tag != out.Tag {
					continue
				}
				matched++
				if row.Capability.Available != available {
					t.Fatalf("nested transport eligibility: %+v", row)
				}
			}
			if matched != 2 {
				t.Fatal("preserved transport resources missing from eligibility")
			}
			if !available {
				for _, target := range []struct {
					category string
					id       uint
					model    any
				}{{"inbounds", in.Id, &model.Inbound{}}, {"outbounds", out.Id, &model.Outbound{}}} {
					// Changing the submitted type cannot turn preservation into an
					// ordinary destructive conversion workflow.
					err := saveeligibility.Validate(db, "edit", target.id, target.model, target.category, "direct")
					var rejected *saveeligibility.Rejection
					expectedReason := reason
					if base := registry.Resolve(target.category, kind); !base.Available() {
						expectedReason = base.Reason
					}
					if !errors.As(err, &rejected) || rejected.ReasonCode() != expectedReason {
						t.Fatal("historical edit admitted", err)
					}
				}
				projection, err := NewSingBoxConfigBuilder(NewRuntimeWithCoreProvider(nil)).BuildProjectionFromDB(db, "{}")
				if err != nil {
					t.Fatal(err)
				}
				if len(projection.Eligibility.Omitted) != 2 {
					t.Fatal("historical transport entered effective runtime")
				}
			}
			var actualIn model.Inbound
			var actualOut model.Outbound
			var actualClient model.Client
			if db.First(&actualIn, in.Id).Error != nil || db.First(&actualOut, out.Id).Error != nil ||
				db.First(&actualClient, client.Id).Error != nil || !bytes.Equal(actualClient.Inbounds, assignment) || !bytes.Equal(actualClient.Config, client.Config) ||
				!bytes.Equal(actualIn.Options, options) || !bytes.Equal(actualOut.Options, options) ||
				!bytes.Equal(actualIn.OutJson, in.OutJson) {
				t.Fatal("eligibility changed preserved owner state")
			}
		})
	}
}
