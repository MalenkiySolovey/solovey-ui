package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/events"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProbeEventPersistenceHasNoRawHTTPRequestFields(t *testing.T) {
	typeOf := reflect.TypeOf(ProbeEventModel{})
	forbidden := []string{"body", "rawpath", "query", "header", "cookie", "authorization", "rawsni", "useragent"}
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("gorm"))
		for _, token := range forbidden {
			if strings.Contains(name, token) {
				t.Fatalf("probe event persistence field %s contains forbidden raw HTTP token %q", field.Name, token)
			}
		}
	}
}

func TestPageOffsetSaturatesInsteadOfOverflowing(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := (PageQuery{Page: maxInt, Limit: 500}).Offset(); got != maxInt {
		t.Fatalf("overflowing page offset = %d, want saturation %d", got, maxInt)
	}
	if got := (PageQuery{Page: 2, Limit: 100}).Offset(); got != 100 {
		t.Fatalf("normal page offset = %d", got)
	}
}

func TestMigrateSettingsAndDropSafety(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Exec("CREATE TABLE unrelated_component_data (id integer primary key, value text)").Error; err != nil {
		t.Fatalf("create unrelated table: %v", err)
	}
	for _, table := range TableModels() {
		if !db.Migrator().HasTable(table.Model) {
			t.Fatalf("table %s was not created", table.Name)
		}
	}
	repository := New(db)
	settings, degraded, err := repository.LoadSettings(context.Background())
	if err != nil || degraded {
		t.Fatalf("LoadSettings: degraded=%v err=%v", degraded, err)
	}
	if settings.DefaultScoreThreshold != domain.DefaultScoreThreshold || settings.ObservationBufferSize != domain.DefaultObservationBufferSize {
		t.Fatalf("unexpected defaults: %#v", settings)
	}
	if err := db.Delete(&SettingsModel{}, "id = ?", 1).Error; err != nil {
		t.Fatalf("remove settings singleton: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("restore migration without settings singleton: %v", err)
	}
	var singleton SettingsModel
	if err := db.First(&singleton, 1).Error; err != nil || singleton.ID != 1 {
		t.Fatalf("settings singleton after restore = %#v err=%v", singleton, err)
	}
	if err := db.Create(&PortOperationModel{
		OperationID: "op-1", State: "applied", FromOwner: "core", ToOwner: "fronting",
		Protocol: "tcp", Listen: "127.0.0.1", Port: 443,
		PreviousResourceJSON: []byte("{}"), NextResourceJSON: []byte("{}"), CreatedAt: 1, UpdatedAt: 1,
	}).Error; err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := DropSchema(db); err == nil {
		t.Fatalf("DropSchema accepted an applied operation")
	}
	if err := db.Model(&PortOperationModel{}).Where("operation_id = ?", "op-1").Update("state", "rolled_back").Error; err != nil {
		t.Fatalf("mark operation rolled back: %v", err)
	}
	if err := DropSchema(db); err != nil {
		t.Fatalf("DropSchema: %v", err)
	}
	for _, table := range TableModels() {
		if db.Migrator().HasTable(table.Model) {
			t.Fatalf("component schema remains after DropSchema: %s", table.Name)
		}
	}
	if !db.Migrator().HasTable("unrelated_component_data") {
		t.Fatal("DropSchema removed data outside server_protection_* tables")
	}
}

func TestDropDataRefusesEveryNonTerminalOperation(t *testing.T) {
	for _, state := range []string{"prepared", "applying", "health", "health_failed", "rolling_back", "lock_suspect"} {
		t.Run(state, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			pid := 42
			if err := db.Create(&OperationLockModel{
				OperationID: "operation-" + state, Kind: "firewall", State: state, Revision: 1,
				LockedByPID: &pid, LockedByInstanceID: "instance", Actor: "admin",
				HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 1, UpdatedAt: 1,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := DropSchema(db); err == nil {
				t.Fatalf("DropSchema accepted non-terminal state %s", state)
			}
		})
	}
}

func TestDropDataRefusesAppliedAndRollbackFailedUntilForgotten(t *testing.T) {
	for _, state := range []string{"applied", "rollback_failed", "reconcile_required"} {
		t.Run(state, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			pid := 42
			operation := OperationLockModel{
				OperationID: "operation-" + state, Kind: "firewall", State: state, Revision: 1,
				LockedByPID: &pid, LockedByInstanceID: "instance", Actor: "admin",
				HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 1, UpdatedAt: 1,
			}
			if err := db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			if err := DropSchema(db); err == nil {
				t.Fatalf("DropSchema accepted %s without explicit forget", state)
			}
			if err := db.Model(&OperationLockModel{}).Where("operation_id = ?", operation.OperationID).Update("state", "forgotten").Error; err != nil {
				t.Fatal(err)
			}
			if err := DropSchema(db); err != nil {
				t.Fatalf("DropSchema after explicit forget: %v", err)
			}
		})
	}
}

func TestArtifactProtectionIncludesHealthAndReconcileRequired(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	pid := 42
	for index, state := range []string{"health", "reconcile_required"} {
		if err := db.Create(&OperationLockModel{OperationID: fmt.Sprintf("operation-artifact-guard-%d", index), Kind: "fronting", State: state,
			Revision: 1, LockedByPID: &pid, LockedByInstanceID: "instance", Actor: "admin", HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	protected, err := New(db).ProtectedArtifactOperations(context.Background())
	if err != nil || len(protected) != 2 || protected["operation-artifact-guard-0"] != "health" || protected["operation-artifact-guard-1"] != "reconcile_required" {
		t.Fatalf("protected=%#v err=%v", protected, err)
	}
}

func TestEventStoreRoundTripAndRetention(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repository := New(db)
	ctx := context.Background()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	prefix := netip.MustParsePrefix("198.51.100.10/32")
	state := scoring.ScoreState{
		ResourceID: "panel:web", SourcePrefix: prefix, CurrentScore: 5, RawScore: 8,
		FirstSeenAt: now.Add(-time.Minute), LastSignalAt: now,
		Reasons:      []scoring.ScoreReason{{Kind: domain.SignalHTTPScannerPath, Count: 1, LastSeen: now, SafeLabel: "scanner_env"}},
		LastDecision: domain.DecisionRecordOnly, LastDedupeKey: "dedupe", LastDedupeAt: now,
		ClassifierPolicyVersion: domain.ClassifierPolicyVersion,
	}
	if err := repository.SaveScore(ctx, state); err != nil {
		t.Fatalf("SaveScore: %v", err)
	}
	loaded, err := repository.LoadScore(ctx, scoring.ScoreKey{ResourceID: state.ResourceID, Prefix: prefix})
	if err != nil {
		t.Fatalf("LoadScore: %v", err)
	}
	if loaded.CurrentScore != state.CurrentScore || loaded.SourcePrefix != prefix || len(loaded.Reasons) != 1 {
		t.Fatalf("loaded score: %#v", loaded)
	}
	batch := make([]events.ProbeEvent, 0, 25)
	for index := 0; index < 25; index++ {
		batch = append(batch, events.ProbeEvent{
			ResourceID: "panel:web", ResourceKind: domain.ResourcePanelWeb,
			SourcePrefix: prefix.String(), IPFamily: 4,
			SignalKind: domain.SignalHTTPScannerPath, ScoreDelta: 3,
			Action:     domain.DecisionRecordOnly,
			SafeMeta:   domain.SafeMeta{PathClass: "scanner_env", StatusClass: "4xx", ClassifierPolicyVersion: 1},
			ObservedAt: now.Add(time.Duration(index) * time.Second), DedupeKey: fmt.Sprintf("event-%d", index),
		})
	}
	if err := repository.AppendBatch(ctx, batch); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	result, err := repository.Purge(ctx, events.RetentionPolicy{GlobalLimit: 100, PerResourceLimit: 20})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if result.EventsRemoved != 5 {
		t.Fatalf("removed events = %d, want 5", result.EventsRemoved)
	}
	var remaining int64
	if err := db.Model(&ProbeEventModel{}).Count(&remaining).Error; err != nil || remaining != 20 {
		t.Fatalf("remaining events = %d, err=%v", remaining, err)
	}
}

func TestV2CompatibilityMigrationIsObserveOnlyAndIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	prefix := netip.MustParsePrefix("198.51.100.10/32")
	event := ProbeEventModel{ResourceID: "core:inbound:1", ResourceKind: string(domain.ResourceInbound), SourceIPCIDR: prefix.String(), SignalKind: string(domain.SignalFallbackHit), ScoreDelta: 5, Action: string(domain.DecisionRouteToDecoy), SafeMetaJSON: json.RawMessage(`{"classifier_policy_version":1}`), SafeMetaBytes: 31, ObservedAt: now.Unix(), DedupeKey: "legacy-event"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	state := scoring.ScoreState{ResourceID: "core:inbound:1", SourcePrefix: prefix, CurrentScore: 10, RawScore: 10, FirstSeenAt: now, LastSignalAt: now, LastDecision: domain.DecisionBlock, ClassifierPolicyVersion: 1}
	if err := New(db).SaveScore(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var signalCount, decisionCount int64
	_ = db.Model(&ProtectionSignalV2Model{}).Count(&signalCount).Error
	_ = db.Model(&ProtectionDecisionV2Model{}).Count(&decisionCount).Error
	if signalCount != 1 || decisionCount != 1 {
		t.Fatalf("v2 counts signals=%d decisions=%d", signalCount, decisionCount)
	}
	var decision ProtectionDecisionV2Model
	if err := db.First(&decision).Error; err != nil {
		t.Fatal(err)
	}
	if decision.ActionImplemented || decision.State != "RESOLVED" || decision.ResolvedIntent != "OBSERVE" {
		t.Fatalf("legacy decision became actionable: %#v", decision)
	}
}

func TestFallbackLeaseRoundTripAndBackupCoverage(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	lease := neutralfallback.ReferenceLeaseV1{Schema: neutralfallback.LeaseSchemaV1, LeaseID: "lease:1", HolderID: "decision:1", ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: 1, RenewedAt: 1, ExpiresAt: 100, State: "ACTIVE"}
	if err := repository.SaveLease(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.LoadLease(context.Background(), lease.LeaseID)
	if err != nil || loaded.ContentDigest != lease.ContentDigest {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	names := map[string]bool{}
	for _, table := range BackupTableModels() {
		names[table.Name] = true
	}
	for _, name := range []string{"server_protection_signals_v2", "server_protection_decisions_v2", "server_protection_fallback_target_leases", "server_protection_recovery_paths_v1"} {
		if !names[name] {
			t.Fatalf("backup omits %s", name)
		}
	}
}

func TestFallbackLeaseUpsertRebindsExactReferenceAndExpiredLoadsStale(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Second)
	lease := neutralfallback.ReferenceLeaseV1{Schema: neutralfallback.LeaseSchemaV1, LeaseID: "fallback-lease:exact", HolderID: "decision:1", ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), State: "ACTIVE"}
	if err := repository.SaveLease(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	lease.PublishRevision = "publish-2"
	lease.ContentDigest = strings.Repeat("b", 64)
	lease.ProviderHealthRevision = "health-2"
	lease.IssuedAt = now.Add(time.Second).Unix()
	lease.RenewedAt = lease.IssuedAt
	lease.ExpiresAt = now.Add(2 * time.Minute).Unix()
	if err := repository.SaveLease(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.LoadLease(context.Background(), lease.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublishRevision != "publish-2" || loaded.ContentDigest != strings.Repeat("b", 64) || loaded.ProviderHealthRevision != "health-2" || loaded.IssuedAt != lease.IssuedAt {
		t.Fatalf("persisted lease identity diverged from acquired reference: %#v", loaded)
	}
	if err := db.Model(&FallbackTargetLeaseModel{}).Where("lease_id = ?", lease.LeaseID).Updates(map[string]any{
		"issued_at":  now.Add(-2 * time.Minute).Unix(),
		"renewed_at": now.Add(-2 * time.Minute).Unix(),
		"expires_at": now.Add(-time.Second).Unix(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err = repository.LoadLease(context.Background(), lease.LeaseID)
	if err != nil || loaded.State != "STALE" {
		t.Fatalf("expired persisted lease remained active: %#v err=%v", loaded, err)
	}
}

func TestV2ContractPersistenceEnforcesHardRetentionCaps(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&SettingsModel{}).Where("id = ?", 1).Updates(map[string]any{"retention_global_limit": 5, "retention_per_resource_limit": 2}).Error; err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	for index := 0; index < 3; index++ {
		observed := base.Add(time.Duration(index) * time.Second)
		signal := domain.ProtectionSignalV2{Schema: domain.ProtectionSignalSchemaV2, Source: domain.SignalSourceV2{SourceID: "fixture", Producer: "fixture", ProducerVersion: "v1", TrustClass: "native", SourceClass: "native"}, Category: domain.SignalCategoryEndpointObservation, Kind: "future_bounded_kind", KnownKind: false, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:1"}, ObservedAt: observed, ExpiresAt: observed.Add(time.Hour), ConfidenceBP: 5000, Provenance: domain.SignalProvenanceV2{AdapterID: "fixture", SourceRevision: "source-1", PolicyRevision: "policy-1"}}
		signal.FinalizeID(fmt.Sprintf("event-%d", index))
		if err := repository.SaveSignalV2(context.Background(), signal); err != nil {
			t.Fatal(err)
		}
	}
	var signalCount int64
	if err := db.Model(&ProtectionSignalV2Model{}).Count(&signalCount).Error; err != nil || signalCount != 2 {
		t.Fatalf("signal retention count=%d err=%v", signalCount, err)
	}
	for index := 0; index < 6; index++ {
		created := base.Add(time.Duration(index) * time.Second)
		decision := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: "policy-1", Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:1"}, TargetResourceIDs: []string{"core:inbound:1"}, SignalRefs: []string{fmt.Sprintf("%064x", index+1)}, SourceClasses: []string{"native"}, ScoreSnapshot: domain.ScoreSnapshotV2{Score: index, TargetGroup: "core:inbound:1", CapturedAt: created}, ConfidenceBP: 5000, RequestedIntent: domain.IntentObserve, CreatedAt: created, ExpiresAt: created.Add(time.Hour), AllowlistResult: domain.PolicyCheckV2{Result: "unknown"}, RecoveryResult: domain.PolicyCheckV2{Result: "unknown"}, CapabilityResolution: domain.CapabilityResolutionV2{Implemented: false, ResolvedIntent: domain.IntentObserve}, State: domain.DecisionResolved}
		decision.FinalizeID()
		if err := repository.SaveDecisionV2(context.Background(), decision); err != nil {
			t.Fatal(err)
		}
	}
	var decisionCount int64
	if err := db.Model(&ProtectionDecisionV2Model{}).Count(&decisionCount).Error; err != nil || decisionCount != 5 {
		t.Fatalf("decision retention count=%d err=%v", decisionCount, err)
	}
}

func TestLeaseCapacityPruningPreservesFreshActiveLease(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []FallbackTargetLeaseModel{
		{LeaseID: "lease:active", HolderID: "decision:active", ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), State: "ACTIVE", ReasonCodesJSON: json.RawMessage("[]")},
		{LeaseID: "lease:stale", HolderID: "decision:stale", ProviderID: "fixture-provider", TargetID: "site:1", PublishRevision: "publish-1", ContentDigest: strings.Repeat("a", 64), ApprovedLocalEndpointID: "endpoint:1", ProviderHealthRevision: "health-1", IssuedAt: now.Add(-2 * time.Minute).Unix(), RenewedAt: now.Add(-2 * time.Minute).Unix(), ExpiresAt: now.Add(-time.Minute).Unix(), State: "STALE", ReasonCodesJSON: json.RawMessage("[]")},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := pruneLeaseContracts(db, 1); err != nil {
		t.Fatal(err)
	}
	var remaining []FallbackTargetLeaseModel
	if err := db.Find(&remaining).Error; err != nil || len(remaining) != 1 || remaining[0].LeaseID != "lease:active" {
		t.Fatalf("lease pruning removed fresh active ownership: %#v err=%v", remaining, err)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "server-protection.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
