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
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
)

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
