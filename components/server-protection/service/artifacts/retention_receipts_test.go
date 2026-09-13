package artifacts

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReceiptHorizonsRunThroughStartupRetentionOwner(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "receipt-retention.db")), &gorm.Config{})
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
	digest := strings.Repeat("a", 64)
	key := fmt.Sprintf("sp-receipt.%010d.startup-pruner", now.Add(-31*24*time.Hour).Unix())
	fronting := protectionrepository.FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest,
		OperationID: "receipt-fronting-terminal", OperationRevision: 1, Status: protectionrepository.FrontingReceiptComplete,
		ResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
	udp := protectionrepository.UDPGuardIdempotencyV1Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest,
		OperationID: "receipt-udp-terminal", OperationRevision: 1, Status: "COMPLETE",
		SemanticResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
	local := protectionrepository.LocalProxyIdempotencyV1Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest,
		OperationID: "receipt-local-terminal", OperationRevision: 1, Status: "COMPLETE",
		SemanticResponseJSON: []byte(`{}`), CreatedAt: 1, UpdatedAt: 1}
	for _, row := range []any{&fronting, &udp, &local} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repository := protectionrepository.New(db)
	storage := artifactTestStorage(t, now)
	result, err := NewPruner(storage, repository, func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFrontingReceipts != 1 || result.DeletedUDPGuardReceipts != 1 || result.DeletedLocalProxyReceipts != 1 || result.DeletedBytes <= 0 {
		t.Fatalf("integrated receipt prune=%#v", result)
	}
	second, err := NewPruner(storage, protectionrepository.New(db), func() time.Time { return now }).Prune(context.Background(), 1, 30)
	if err != nil || second.DeletedFrontingReceipts != 0 || second.DeletedUDPGuardReceipts != 0 || second.DeletedLocalProxyReceipts != 0 {
		t.Fatalf("integrated receipt restart prune=%#v err=%v", second, err)
	}
}
