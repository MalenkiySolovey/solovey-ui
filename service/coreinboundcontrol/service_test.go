package coreinboundcontrol

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceLoadsSnapshotsAndExactTLSReferenceCount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:core-inbound-control?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	tlsRecord := model.Tls{Id: 7, Name: "shared", Server: json.RawMessage(`{"enabled":true}`)}
	if err = db.Create(&tlsRecord).Error; err != nil {
		t.Fatal(err)
	}
	for _, inbound := range []model.Inbound{
		{Id: 1, SortOrder: 2, Type: "trojan", Tag: "second", TlsId: 7, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":444,"fallback":{"server":"127.0.0.1","server_port":8080}}`)},
		{Id: 2, SortOrder: 1, Type: "trojan", Tag: "first", TlsId: 7, Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":443,"fallback":{"server":"127.0.0.1","server_port":8080}}`)},
	} {
		if err = db.Create(&inbound).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: db, identity: exactIdentity(true)}
	snapshots, err := service.ListSnapshots(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].InboundDatabaseID != 2 || snapshots[0].TLSReferenceCount != 2 || snapshots[1].TLSReferenceCount != 2 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	snapshot, err := service.Snapshot(t.Context(), 1)
	if err != nil || snapshot.InboundDatabaseID != 1 || snapshot.TLSReferenceCount != 2 {
		t.Fatalf("snapshot = %#v, err=%v", snapshot, err)
	}
}

func TestAuthenticationMembershipRequiresProductionSchema(t *testing.T) {
	fixture := newPatchFixture(t, trojanRow())
	request := previewRequest(t, fixture, FallbackPatchTrojanDefaultTCP, plaintextEndpoint(), false)
	var before model.Inbound
	if err := fixture.db.First(&before, fixture.runtimeInboundID()).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Migrator().DropTable(&model.Client{}); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.service.Snapshot(t.Context(), fixture.runtimeInboundID()); !errors.Is(err, ErrAuthenticationMembershipUnavailable) {
		t.Fatalf("snapshot error = %v", err)
	}
	if _, err := fixture.service.ListSnapshots(t.Context(), 10); !errors.Is(err, ErrAuthenticationMembershipUnavailable) {
		t.Fatalf("list error = %v", err)
	}
	if _, err := fixture.service.PreviewFallbackPatch(t.Context(), request); !IsAdapterError(err, ErrorDatabase) {
		t.Fatalf("preview error = %v", err)
	}
	var after model.Inbound
	if err := fixture.db.First(&after, fixture.runtimeInboundID()).Error; err != nil {
		t.Fatal(err)
	}
	var checkpoints int64
	if err := fixture.db.Model(&model.InboundFallbackCheckpoint{}).Count(&checkpoints).Error; err != nil {
		t.Fatal(err)
	}
	if string(after.Options) != string(before.Options) || checkpoints != 0 || fixture.coordinator.calls != 0 ||
		fixture.runtime.applyCalls != 0 || fixture.hooks.beforeCalls != 0 || fixture.hooks.afterCalls != 0 || fixture.validator.calls != 0 {
		t.Fatalf("missing membership schema changed state: before=%s after=%s checkpoints=%d coordinator=%d runtime=%d hooks=%d/%d validator=%d",
			before.Options, after.Options, checkpoints, fixture.coordinator.calls, fixture.runtime.applyCalls,
			fixture.hooks.beforeCalls, fixture.hooks.afterCalls, fixture.validator.calls)
	}

	if err := fixture.db.AutoMigrate(&model.Client{}); err != nil {
		t.Fatal(err)
	}
	empty, err := fixture.service.Snapshot(t.Context(), fixture.runtimeInboundID())
	if err != nil || empty.Authentication.Count != 0 || empty.Authentication.Expected {
		t.Fatalf("authoritative empty membership = %#v, err=%v", empty.Authentication, err)
	}
	if err := fixture.db.Create(&model.Client{Name: "member", Inbounds: json.RawMessage(`[32]`)}).Error; err != nil {
		t.Fatal(err)
	}
	populated, err := fixture.service.Snapshot(t.Context(), fixture.runtimeInboundID())
	if err != nil || populated.Authentication.Count != 1 || !populated.Authentication.Expected {
		t.Fatalf("authoritative populated membership = %#v, err=%v", populated.Authentication, err)
	}
}
