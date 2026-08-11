package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestLegacySelfStealProfileBecomesDisabledUnmanagedCandidate(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	port := 443
	targetPort := 8443
	legacy := ProfileModel{
		ResourceID: "core:inbound:legacy", ResourceKind: "inbound", ResourceOwner: "core",
		InboundTag: "matching-looking", Enabled: true, Status: "active", Mode: string(domain.ProfileModeSelfSteal),
		FallbackResourceID: "fallback:site:1", PublicListen: "203.0.113.10", PublicPort: &port,
		HandshakeHost: "front.example", HandshakeTargetHost: "127.0.0.1", HandshakeTargetPort: &targetPort,
		ScoreThreshold: 5, GraylistTTLSeconds: 3600, DefaultAction: "record_only",
		ManagedFirewall: true, Revision: 7, CreatedAt: 10, UpdatedAt: 10,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2000, 0).UTC()
	if err := ReconcileLegacySelfStealProfiles(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	var migrated ProfileModel
	if err := db.First(&migrated, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.Mode != string(domain.ProfileModeNativeFallback) || migrated.Enabled || migrated.Status != "disabled" ||
		migrated.MigrationCandidate != string(domain.ProfileModeNativeFallback) ||
		migrated.MigrationReason != LegacySelfStealRequiresRepreview ||
		migrated.LegacyTopologyState != LegacyTopologyUnmanagedExisting {
		t.Fatalf("migrated profile=%#v", migrated)
	}
	if migrated.InboundTag != "" || migrated.FallbackResourceID != "" || migrated.PublicListen != "" ||
		migrated.PublicPort != nil || migrated.HandshakeHost != "" || migrated.HandshakeTargetHost != "" ||
		migrated.HandshakeTargetPort != nil || migrated.ManagedFirewall {
		t.Fatalf("legacy selections remain actionable: %#v", migrated)
	}
	for table, model := range map[string]any{
		"native state": &NativeFallbackStateModel{}, "native operation": &NativeFallbackOperationModel{},
		"reservation mirror": &FallbackTargetLeaseModel{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	updatedAt := migrated.UpdatedAt
	if err := ReconcileLegacySelfStealProfiles(context.Background(), db, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migrated, legacy.ID).Error; err != nil || migrated.UpdatedAt != updatedAt {
		t.Fatalf("idempotent profile reconciliation=%#v err=%v", migrated, err)
	}
}

func TestLegacySelfStealProfileCancellationDoesNotChangeRow(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	row := ProfileModel{
		ResourceID: "core:inbound:cancel", ResourceKind: "inbound", ResourceOwner: "core",
		Enabled: true, Status: "active", Mode: string(domain.ProfileModeSelfSteal),
		ScoreThreshold: 5, GraylistTTLSeconds: 3600, DefaultAction: "record_only",
		Revision: 1, CreatedAt: 10, UpdatedAt: 10,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReconcileLegacySelfStealProfiles(ctx, db, time.Unix(2000, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration err=%v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Mode != string(domain.ProfileModeSelfSteal) || !row.Enabled || row.UpdatedAt != 10 {
		t.Fatalf("canceled migration changed row: %#v", row)
	}
}
