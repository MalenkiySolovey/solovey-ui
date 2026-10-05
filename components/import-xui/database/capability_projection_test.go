//go:build !minimal

package importxui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func unavailableImportTarget(t *testing.T, category, panelType string) entitycapabilities.Snapshot {
	t.Helper()
	snapshot := entitycapabilities.Current()
	for index := range snapshot.Facts {
		fact := &snapshot.Facts[index]
		if fact.Category == category && fact.Type == panelType {
			fact.Compiled = false
			fact.Available = false
			fact.Reason = "KNOWN_BUT_NOT_COMPILED"
			return snapshot
		}
	}
	t.Fatal("missing authoritative fixture capability")
	return snapshot
}

func TestCapabilityImportUnavailableBeforeMalformedSourceMapping(t *testing.T) {
	for _, test := range []struct{ panelType, reason string }{
		{"vless", "KNOWN_BUT_NOT_COMPILED"},
		{" VLESS ", "KNOWN_BUT_NOT_COMPILED"},
		{"historical-protocol", "UNKNOWN_ENTITY_TYPE"},
		{"warp", "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT"},
		{"anytls", "IMPORT_MAPPING_UNSUPPORTED"},
	} {
		t.Run(test.panelType, func(t *testing.T) {
			initPlanExtraMainDB(t)
			before := map[string]int64{}
			for _, table := range []string{"inbounds", "endpoints", "clients", "tls"} {
				before[table] = destinationRowCount(t, table)
			}
			snapshot := unavailableImportTarget(t, "inbounds", "vless")
			row := validPlanExtraInbound()
			row.protocol, row.settings = test.panelType, "malformed-source-settings"
			src := createPlanExtraSource(t, []planExtraInbound{row})
			sourceDB, err := gorm.Open(sqlite.Open(src), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := sourceDB.Exec("UPDATE inbounds SET stream_settings = ?", "malformed-source-tls").Error; err != nil {
				t.Fatal(err)
			}
			sqlDB, err := sourceDB.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
			plan, err := Plan(src, PlanOptions{TargetCapabilities: &snapshot})
			if err != nil {
				t.Fatalf("unavailable row reached source mapping: %v", err)
			}
			if len(plan.Items) != 1 {
				t.Fatalf("unavailable row produced dependent items: %d", len(plan.Items))
			}
			item := plan.Items[0]
			if item.Action != ActionSkip || string(item.PreviewJSON) != "null" || len(item.Unsupported) != 1 || item.Unsupported[0].Reason != test.reason {
				t.Fatal("unavailable row was not explicitly excluded with its authoritative reason")
			}
			if item.Unsupported[0].Capability.Available != (test.reason == "IMPORT_MAPPING_UNSUPPORTED") {
				t.Fatal("mapping eligibility overwrote runtime capability truth")
			}
			report, err := Apply(src, *plan, ApplyOptions{TargetCapabilities: &snapshot, SkipBackup: true, SkipAudit: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Unsupported) != 1 || report.Unsupported[0].Reason != test.reason {
				t.Fatal("Apply lost canonical unsupported diagnosis")
			}
			for _, table := range []string{"inbounds", "endpoints", "clients", "tls"} {
				if count := destinationRowCount(t, table); count != before[table] {
					t.Fatalf("excluded import wrote %s: %d", table, count)
				}
			}
		})
	}
}

func TestCapabilityImportPlanCannotAuthorizeUnavailableTarget(t *testing.T) {
	initPlanExtraMainDB(t)
	src := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	livePlan, err := Plan(src, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := unavailableImportTarget(t, "inbounds", "vless")
	if _, err := Apply(src, *livePlan, ApplyOptions{TargetCapabilities: &snapshot, SkipBackup: true}); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("old plan survived target eligibility change: %v", err)
	}
	plan, err := Plan(src, PlanOptions{TargetCapabilities: &snapshot})
	if err != nil {
		t.Fatal(err)
	}
	forged := cloneMigrationPlan(*plan)
	forged.Items[0].Unsupported = nil
	forged.Items[0].PreviewJSON = []byte(`{"type":"vless","tag":"forged"}`)
	canonical, err := validateSubmittedPlan(context.Background(), dbsqlite.DB(), src, forged, &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical.Items[0].Unsupported) != 1 || string(canonical.Items[0].PreviewJSON) != "null" {
		t.Fatal("submitted capability metadata survived canonical reconstruction")
	}
	forged.Items[0].Action = ActionCreate
	if _, err := Apply(src, forged, ApplyOptions{TargetCapabilities: &snapshot, SkipBackup: true}); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("forged unsupported choice became runnable: %v", err)
	}
	if destinationRowCount(t, "inbounds") != 0 || destinationRowCount(t, "clients") != 0 {
		t.Fatal("rejected plan persisted runtime state")
	}
}

func TestCapabilityImportRoutingExcludesWholeUnavailableCandidate(t *testing.T) {
	initPlanExtraMainDB(t)
	beforeOutbounds, beforeEndpoints := destinationRowCount(t, "outbounds"), destinationRowCount(t, "endpoints")
	src := createPlanExtraSource(t, nil)
	sourceDB, err := gorm.Open(sqlite.Open(src), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Exec("CREATE TABLE settings (id integer primary key, key text, value text)").Error; err != nil {
		t.Fatal(err)
	}
	xray := `{"outbounds":[{"tag":"chain","protocol":"vless","settings":{"vnext":[{"address":"fixture.example","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001"}]}]}}],"routing":{"rules":[{"type":"field","domain":["fixture.example"],"outboundTag":"chain"}]}}`
	if err := sourceDB.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", "xrayConfig", xray).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sourceDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := unavailableImportTarget(t, "outbounds", "vless")
	plan, err := Plan(src, PlanOptions{TargetCapabilities: &snapshot, IncludeRouting: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Kind != KindRouting || plan.Items[0].Action != ActionSkip || len(plan.Items[0].Unsupported) != 1 || string(plan.Items[0].PreviewJSON) != "null" {
		t.Fatal("routing candidate containing an unavailable outbound was partially accepted")
	}
	report, err := Apply(src, *plan, ApplyOptions{TargetCapabilities: &snapshot, SkipBackup: true, SkipAudit: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 1 || destinationRowCount(t, "outbounds") != beforeOutbounds || destinationRowCount(t, "endpoints") != beforeEndpoints {
		t.Fatal("excluded routing candidate wrote dependent runtime objects")
	}
}

func TestCapabilityImportRetainsAdapterProtocolNormalization(t *testing.T) {
	initPlanExtraMainDB(t)
	row := validPlanExtraInbound()
	row.protocol = " VLESS "
	src := createPlanExtraSource(t, []planExtraInbound{row})
	plan, err := Plan(src, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(unsupportedObjects(*plan)) != 0 {
		t.Fatal("existing mapper normalization was rejected by capability projection")
	}
	if _, err := Apply(src, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true}); err != nil {
		t.Fatal(err)
	}
	if destinationRowCount(t, "inbounds") != 1 || destinationRowCount(t, "clients") != 1 {
		t.Fatal("normalized source did not produce its accepted entities")
	}
}

func TestCapabilityImportDestinationUsesLiveAuthoritativeFacts(t *testing.T) {
	for _, fact := range entitycapabilities.Current().Facts {
		if fact.Category != "inbounds" && fact.Category != "outbounds" && fact.Category != "endpoints" {
			continue
		}
		if err := checkImportedCapability(fact.Category, fact.Type); (err == nil) != fact.Available {
			t.Fatalf("destination adapter disagrees with %s/%s", fact.Category, fact.Type)
		}
	}
	if checkImportedCapability("inbounds", "historical-protocol") == nil {
		t.Fatal("unknown destination type accepted")
	}
}

func TestCapabilityImportClientTrafficRetainsOnlyAvailableBindings(t *testing.T) {
	initPlanExtraMainDB(t)
	supported := validPlanExtraInbound()
	excluded := planExtraInbound{id: 2, port: 24444, protocol: "hysteria2", tag: "excluded", settings: "malformed-excluded-settings"}
	src := createPlanExtraSource(t, []planExtraInbound{supported, excluded})
	sourceDB, err := gorm.Open(sqlite.Open(src), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, traffic := range []struct {
		inbound int
		email   string
		up      int
	}{
		{1, "alice", 1}, {2, "alice", 2}, {2, "metadata-only", 3},
	} {
		if err := sourceDB.Exec("INSERT INTO client_traffics (inbound_id, enable, email, up, down, total, all_time, expiry_time, reset, last_online) VALUES (?, 1, ?, ?, 0, 0, 0, 0, 0, 0)", traffic.inbound, traffic.email, traffic.up).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := sourceDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := unavailableImportTarget(t, "inbounds", "hysteria2")
	plan, err := Plan(src, PlanOptions{TargetCapabilities: &snapshot})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Apply(src, *plan, ApplyOptions{TargetCapabilities: &snapshot, SkipBackup: true, SkipAudit: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 1 || report.Summary.Inbounds.Imported != 1 {
		t.Fatal("mixed eligibility classification was lost")
	}
	var inbound model.Inbound
	if err := dbsqlite.DB().Where("tag = ?", supported.tag).First(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	for _, expectation := range []struct {
		name     string
		bindings int
		up       int64
	}{
		{"alice", 1, 3}, {"metadata-only", 0, 3},
	} {
		var client model.Client
		if err := dbsqlite.DB().Where("name = ?", expectation.name).First(&client).Error; err != nil {
			t.Fatal(err)
		}
		var bindings []uint
		if err := json.Unmarshal(client.Inbounds, &bindings); err != nil {
			t.Fatal(err)
		}
		if len(bindings) != expectation.bindings || (len(bindings) == 1 && bindings[0] != inbound.Id) {
			t.Fatal("excluded source leaked an invalid client binding")
		}
		if client.Up != expectation.up {
			t.Fatal("capability projection changed retained traffic metadata")
		}
	}
}
