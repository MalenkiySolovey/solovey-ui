package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestArtifactsFirewallGenerationRetentionIntegration(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "artifacts-retention.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	storage := artifactTestStorage(t, now.Add(-60*24*time.Hour))
	current := "operation-00000000000000000000000000000501"
	otherLive := "operation-00000000000000000000000000000502"
	superseded := "operation-00000000000000000000000000000503"
	pid := 42
	for index, operationID := range []string{current, otherLive, superseded} {
		state := "applied"
		if operationID == otherLive {
			state = "rolled_back"
		}
		operation := protectionrepository.OperationLockModel{OperationID: operationID, Kind: "firewall", State: state, Revision: index + 1, LockedByPID: &pid, LockedByInstanceID: "instance", Actor: "admin", HeartbeatAt: 1, ExpiresAt: 2, CreatedAt: 1, UpdatedAt: 1}
		if err := db.Create(&operation).Error; err != nil {
			t.Fatal(err)
		}
		transitionState := "APPLIED"
		if state == "rolled_back" {
			transitionState = "ROLLED_BACK"
		}
		transition := protectionrepository.FirewallContributionTransitionModel{OperationID: operationID, Schema: "fixture", ContributionID: "contribution", PreviousJSON: json.RawMessage(`{}`), DesiredSemanticRevision: strings.Repeat("a", 64), DesiredJSON: json.RawMessage(`{}`), ManagedPlanRevision: strings.Repeat("b", 64), CandidateSHA256: strings.Repeat("c", 64), State: transitionState, CreatedAt: 1, UpdatedAt: 1}
		if err := db.Create(&transition).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := (Service{Storage: storage, Store: repository}).WriteRevision(context.Background(), operationID, "artifacts-"+operationID[len(operationID)-3:], map[string][]byte{"state.json": []byte(`{"safe":true}`)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, contribution := range []protectionrepository.FirewallContributionModel{
		{ContributionID: "baseline", Schema: "fixture", Kind: "BASELINE", ResourceID: "managed-table", Network: "inet", AddressFamily: "inet", SemanticRevision: strings.Repeat("d", 64), SemanticJSON: json.RawMessage(`{}`), AppliedOperationID: current, CreatedAt: 1, UpdatedAt: 1},
		{ContributionID: "other-live", Schema: "fixture", Kind: "UDP_DIRECT_GUARDED", ResourceID: "resource", Network: "udp", AddressFamily: "ipv4", SemanticRevision: strings.Repeat("e", 64), SemanticJSON: json.RawMessage(`{}`), AppliedOperationID: otherLive, CreatedAt: 1, UpdatedAt: 1},
	} {
		if err := db.Create(&contribution).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&protectionrepository.FirewallCompositionModel{ID: 1, Schema: "fixture", Revision: strings.Repeat("f", 64), ManagedPlanRevision: strings.Repeat("1", 64), CandidateSHA256: strings.Repeat("2", 64), BindingsJSON: json.RawMessage(`[]`), State: "ACTIVE", AppliedOperationID: current, UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := NewPruner(storage, repository, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFilesets != 1 || result.DeletedOperations != 1 || result.DeletedTransitions != 1 {
		t.Fatalf("integrated prune result = %#v", result)
	}
	for _, operationID := range []string{current, otherLive} {
		if _, err := repository.ArtifactByOperation(context.Background(), operationID); err != nil {
			t.Fatalf("live artifact %s missing: %v", operationID, err)
		}
		if err := db.First(&protectionrepository.OperationLockModel{}, "operation_id = ?", operationID).Error; err != nil {
			t.Fatalf("live operation %s missing: %v", operationID, err)
		}
	}
	if _, err := repository.ArtifactByOperation(context.Background(), superseded); !errors.Is(err, protectionrepository.ErrRecordNotFound) {
		t.Fatalf("superseded artifact survived: %v", err)
	}
	if err := db.First(&protectionrepository.OperationLockModel{}, "operation_id = ?", superseded).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("superseded operation survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storage.Root(), "revisions", "artifacts-503")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded fileset survived: %v", err)
	}
	second, err := NewPruner(storage, repository, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil || second.DeletedFilesets != 0 || second.DeletedOperations != 0 || second.DeletedTransitions != 0 || second.DeletedOrphans != 0 {
		t.Fatalf("integrated restart prune was not idempotent: %#v err=%v", second, err)
	}
}
