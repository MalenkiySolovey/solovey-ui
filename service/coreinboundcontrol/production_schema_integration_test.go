package coreinboundcontrol

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestProductionSQLiteMembershipFeedsNativeFallbackPreview(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panel.db")
	if err := dbsqlite.Init(databasePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Errorf("close production database: %v", err)
		}
	})
	db := dbsqlite.DB()
	if !db.Migrator().HasTable(&model.Client{}) {
		t.Fatal("production migration did not install clients table")
	}

	inbound := realityRow()
	tlsRecord := *inbound.Tls
	inbound.Tls = nil
	if err := db.Create(&tlsRecord).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Client{
		Name: "production-member", SubSecret: "production-member-subscription", Inbounds: json.RawMessage(`[31]`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	validator := &fakePatchValidator{}
	hydrator := &fakeCandidateHydrator{}
	service := NewWithMutations(db, nil, MutationDependencies{
		Hydrator: hydrator, validator: validator,
		now: func() time.Time { return time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC) },
	})
	service.identity = exactIdentity(true)
	snapshot, err := service.Snapshot(t.Context(), inbound.Id)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Authentication.Count != 1 || !snapshot.Authentication.Expected {
		t.Fatalf("production membership was not projected: %#v", snapshot.Authentication)
	}

	endpoint := tlsEndpoint()
	preview, err := service.PreviewFallbackPatch(t.Context(), PreviewFallbackPatchRequestV1{
		Expected: FallbackPatchExpectationsV1{
			InboundDatabaseID: snapshot.InboundDatabaseID, ResourceID: snapshot.ResourceID,
			ConfigurationRevision: snapshot.ConfigurationRevision, RuntimeIdentityRevision: snapshot.RuntimeIdentityRevision,
			CapabilityResolverRevision: snapshot.CapabilityResolverRevision, EndpointRevision: endpoint.EndpointRevision,
		},
		Variant: FallbackPatchVLESSRealityHandshakeTCP, ApprovedEndpoint: endpoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Digest == "" || preview.InboundDatabaseID != inbound.Id || validator.calls != 1 || hydrator.calls < 2 {
		t.Fatalf("production-schema preview = %#v, validator=%d hydrator=%d", preview, validator.calls, hydrator.calls)
	}
	var stored model.Inbound
	if err := db.First(&stored, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if string(stored.Options) != string(inbound.Options) {
		t.Fatal("native-fallback preview mutated the production inbound")
	}
}
