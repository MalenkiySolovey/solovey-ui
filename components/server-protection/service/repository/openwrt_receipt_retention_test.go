package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openwrtdeployment "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenWrtFeatureReceiptStoresRemainBoundedAcrossRestart(t *testing.T) {
	profile := openwrtdeployment.DefaultProfile()
	if err := profile.Validate(); err != nil || profile.DatabaseFolder != openwrtdeployment.DefaultDatabaseFolder {
		t.Fatalf("locked OpenWrt persistent database profile=%#v err=%v", profile, err)
	}
	databasePath := filepath.Join(t.TempDir(), "receipts-receipt-openwrt.db")
	db := openReceiptOpenWrtDatabase(t, databasePath)
	now := time.Now().UTC().Truncate(time.Second)
	digest := strings.Repeat("a", 64)
	oldestKey := ""

	for cycle := 0; cycle < 12; cycle++ {
		for index := 0; index < 6; index++ {
			issuedAt := now.Add(time.Duration(cycle*10+index-200) * time.Second)
			key := fmt.Sprintf("sp-receipt.%010d.openwrt-%02d-%02d", issuedAt.Unix(), cycle, index)
			if cycle == 0 && index == 0 {
				oldestKey = key
			}
			status := "COMPLETE"
			if index%2 == 0 {
				status = "AMBIGUOUS"
			}
			operationID := fmt.Sprintf("receipt-openwrt-terminal-%02d-%02d", cycle, index)
			updatedAt := issuedAt.Unix()
			for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
				insertReceiptReceiptFamily(t, db, family, key, digest, status, operationID, updatedAt, 32)
			}
		}
		result, err := New(db).PruneIdempotencyReceipts(context.Background(), 2, now.Add(-30*24*time.Hour).Unix(), 8<<20)
		expectedDeleted := 6
		if cycle == 0 {
			expectedDeleted = 4
		}
		if err != nil || result.DeletedFrontingReceipts != expectedDeleted || result.DeletedUDPGuardReceipts != expectedDeleted || result.DeletedLocalProxyReceipts != expectedDeleted {
			t.Fatalf("OpenWrt receipt retention cycle %d = %#v err=%v", cycle, result, err)
		}
	}
	closeReceiptOpenWrtDatabase(t, db)

	db = openReceiptOpenWrtDatabase(t, databasePath)
	t.Cleanup(func() { closeReceiptOpenWrtDatabase(t, db) })
	for _, model := range []any{&FrontingIdempotencyV2Model{}, &UDPGuardIdempotencyV1Model{}, &LocalProxyIdempotencyV1Model{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("OpenWrt %T retained count=%d err=%v", model, count, err)
		}
	}
	var fences int64
	if err := db.Model(&IdempotencyReplayFenceV1Model{}).Count(&fences).Error; err != nil || fences != 3 {
		t.Fatalf("OpenWrt replay fences=%d err=%v", fences, err)
	}
	repository := New(db)
	if _, err := repository.FrontingReceiptV2(context.Background(), "apply", oldestKey); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("OpenWrt fronting expired-key result=%v", err)
	}
	if _, _, err := repository.BeginUDPGuardReceipt(context.Background(), "apply", oldestKey, digest); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("OpenWrt UDP expired-key result=%v", err)
	}
	if _, _, err := repository.ReplayLocalProxyReceipt(context.Background(), "apply", oldestKey, digest); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("OpenWrt local-proxy expired-key result=%v", err)
	}
}

func openReceiptOpenWrtDatabase(t *testing.T, databasePath string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		closeReceiptOpenWrtDatabase(t, db)
		t.Fatal(err)
	}
	return db
}

func closeReceiptOpenWrtDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}
