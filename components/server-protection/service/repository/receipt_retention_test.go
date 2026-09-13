package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestReceiptRetentionBoundsEachFamilyAndKeepsLiveClosure(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	digest := strings.Repeat("a", 64)
	for index := 0; index < 8; index++ {
		status := "COMPLETE"
		if index%2 == 0 {
			status = "AMBIGUOUS"
		}
		key := receiptReceiptKey(now.Add(-time.Duration(20-index)*time.Second), fmt.Sprintf("count-%02d", index))
		operationID := fmt.Sprintf("receipt-terminal-%02d", index)
		insertReceiptReceiptFamily(t, db, receiptFamilyFronting, key, digest, status, operationID, now.Unix()-int64(20-index), 256)
		insertReceiptReceiptFamily(t, db, receiptFamilyUDPGuard, key, digest, status, operationID, now.Unix()-int64(20-index), 256)
		insertReceiptReceiptFamily(t, db, receiptFamilyLocalProxy, key, digest, status, operationID, now.Unix()-int64(20-index), 256)
	}
	for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
		insertReceiptReceiptFamily(t, db, family, "legacy-pending-"+family, digest, "PENDING", "", now.Unix()-100, 2)
		insertReceiptReceiptFamily(t, db, family, "legacy-ambiguous-"+family, digest, "AMBIGUOUS", "", now.Unix()-100, 2)
		insertReceiptReceiptFamily(t, db, family, "legacy-live-"+family, digest, "COMPLETE", "receipt-live-"+family, now.Unix()-100, 2)
	}
	for index, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
		lock := OperationLockModel{OperationID: "receipt-live-" + family, Kind: []string{"fronting", "firewall", "local_proxy"}[index],
			ResourceID: "receipt:" + family, Protocol: "test", State: "applying", Revision: 1,
			IdempotencyKey: "receipt-live-lock-" + family, Actor: "receipt", CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
		if err := db.Create(&lock).Error; err != nil {
			t.Fatal(err)
		}
	}

	result, err := New(db).PruneIdempotencyReceipts(context.Background(), 2, now.Add(-30*24*time.Hour).Unix(), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFrontingReceipts != 6 || result.DeletedUDPGuardReceipts != 6 || result.DeletedLocalProxyReceipts != 6 || result.DeletedBytes <= 0 {
		t.Fatalf("count retention = %#v", result)
	}
	for _, model := range []any{&FrontingIdempotencyV2Model{}, &UDPGuardIdempotencyV1Model{}, &LocalProxyIdempotencyV1Model{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 5 {
			t.Fatalf("%T retained count=%d err=%v", model, count, err)
		}
	}

	newestKey := receiptReceiptKey(now.Add(-13*time.Second), "count-07")
	if receipt, err := New(db).FrontingReceiptV2(context.Background(), "apply", newestKey); err != nil || receipt.Status != FrontingReceiptComplete {
		t.Fatalf("fronting replay inside horizon=%#v err=%v", receipt, err)
	}
	if receipt, replay, err := New(db).BeginUDPGuardReceipt(context.Background(), "apply", newestKey, digest); err != nil || !replay || receipt.Status != "COMPLETE" {
		t.Fatalf("UDP replay inside horizon=%#v replay=%v err=%v", receipt, replay, err)
	}
	if receipt, replay, err := New(db).ReplayLocalProxyReceipt(context.Background(), "apply", newestKey, digest); err != nil || !replay || receipt.Status != "COMPLETE" {
		t.Fatalf("local-proxy replay inside horizon=%#v replay=%v err=%v", receipt, replay, err)
	}

	expiredKey := receiptReceiptKey(now.Add(-20*time.Second), "count-00")
	assertReceiptExpiredAcrossFamilies(t, New(db), expiredKey, digest)
	newKey := receiptReceiptKey(now, "new-key-0001")
	fronting := FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: newKey, RequestDigest: digest, Status: FrontingReceiptPending,
		ResponseJSON: []byte(`{}`), CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if _, joined, err := New(db).ClaimFrontingReceiptV2(context.Background(), fronting); err != nil || joined {
		t.Fatalf("fronting new-key contract joined=%v err=%v", joined, err)
	}
	if _, replay, err := New(db).BeginUDPGuardReceipt(context.Background(), "apply", newKey, digest); err != nil || replay {
		t.Fatalf("UDP new-key contract replay=%v err=%v", replay, err)
	}
	if _, replay, err := New(db).BeginLocalProxyReceipt(context.Background(), "apply", newKey, digest); err != nil || replay {
		t.Fatalf("local-proxy new-key contract replay=%v err=%v", replay, err)
	}
}

func TestReceiptRetentionEnforcesAgeAndLogicalBytesPerFamily(t *testing.T) {
	for _, mode := range []string{"age", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			db := openTestDB(t)
			if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			updatedAt := now.Unix()
			keepBytes := int64(8 << 20)
			if mode == "age" {
				updatedAt = now.Add(-31 * 24 * time.Hour).Unix()
			} else {
				keepBytes = 1
			}
			for index := 0; index < 4; index++ {
				status := "COMPLETE"
				if index%2 == 1 {
					status = "AMBIGUOUS"
				}
				key := receiptReceiptKey(time.Unix(updatedAt-int64(index), 0), fmt.Sprintf("%s-%02d", mode, index))
				operationID := fmt.Sprintf("receipt-%s-terminal-%02d", mode, index)
				for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
					insertReceiptReceiptFamily(t, db, family, key, strings.Repeat("b", 64), status, operationID, updatedAt-int64(index), 2048)
				}
			}
			result, err := New(db).PruneIdempotencyReceipts(context.Background(), 100, now.Add(-30*24*time.Hour).Unix(), keepBytes)
			if err != nil || result.DeletedFrontingReceipts != 4 || result.DeletedUDPGuardReceipts != 4 || result.DeletedLocalProxyReceipts != 4 {
				t.Fatalf("%s retention=%#v err=%v", mode, result, err)
			}
		})
	}
}

func TestReceiptRetentionPreservesRestoredPendingAsAmbiguous(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	digest := strings.Repeat("c", 64)
	for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
		insertReceiptReceiptFamily(t, db, family, receiptReceiptKey(now, "restored-"+family), digest, "PENDING", "", now.Unix(), 2)
	}
	if err := ReconcileRestoredFrontingRecords(context.Background(), db, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestoredUDPGuardRecords(context.Background(), db, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestoredLocalProxyRecords(context.Background(), db, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	result, err := New(db).PruneIdempotencyReceipts(context.Background(), 1, now.Add(2*time.Second).Unix(), 1)
	if err != nil || result.DeletedFrontingReceipts != 0 || result.DeletedUDPGuardReceipts != 0 || result.DeletedLocalProxyReceipts != 0 {
		t.Fatalf("restored ambiguity retention=%#v err=%v", result, err)
	}
	for _, model := range []any{&FrontingIdempotencyV2Model{}, &UDPGuardIdempotencyV1Model{}, &LocalProxyIdempotencyV1Model{}} {
		var count int64
		if err := db.Model(model).Where("status = ?", "AMBIGUOUS").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%T restored ambiguity count=%d err=%v", model, count, err)
		}
	}
}

func TestReceiptRetentionDeleteFaultRollsBackReceiptsAndReplayFences(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	digest := strings.Repeat("d", 64)
	key := receiptReceiptKey(now.Add(-time.Hour), "interrupted-prune")
	for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
		insertReceiptReceiptFamily(t, db, family, key, digest, "COMPLETE", "receipt-interrupted", now.Add(-time.Hour).Unix(), 256)
	}
	if err := db.Exec(`CREATE TRIGGER receipt_fail_udp_delete BEFORE DELETE ON server_protection_udp_guard_idempotency_v1 BEGIN SELECT RAISE(ABORT, 'receipt injected delete fault'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db).PruneIdempotencyReceipts(context.Background(), 1, now.Unix(), 8<<20); err == nil {
		t.Fatal("injected receipt delete fault was acknowledged")
	}
	for _, model := range []any{&FrontingIdempotencyV2Model{}, &UDPGuardIdempotencyV1Model{}, &LocalProxyIdempotencyV1Model{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%T transactional rollback count=%d err=%v", model, count, err)
		}
	}
	var fences int64
	if err := db.Model(&IdempotencyReplayFenceV1Model{}).Count(&fences).Error; err != nil || fences != 0 {
		t.Fatalf("replay fence escaped failed transaction: count=%d err=%v", fences, err)
	}
	if err := db.Exec(`DROP TRIGGER receipt_fail_udp_delete`).Error; err != nil {
		t.Fatal(err)
	}
	result, err := New(db).PruneIdempotencyReceipts(context.Background(), 1, now.Unix(), 8<<20)
	if err != nil || result.DeletedFrontingReceipts != 1 || result.DeletedUDPGuardReceipts != 1 || result.DeletedLocalProxyReceipts != 1 {
		t.Fatalf("restart prune=%#v err=%v", result, err)
	}
	assertReceiptExpiredAcrossFamilies(t, New(db), key, digest)
	second, err := New(db).PruneIdempotencyReceipts(context.Background(), 1, now.Unix(), 8<<20)
	if err != nil || second.DeletedFrontingReceipts != 0 || second.DeletedUDPGuardReceipts != 0 || second.DeletedLocalProxyReceipts != 0 {
		t.Fatalf("idempotent restart prune=%#v err=%v", second, err)
	}
}

func TestReceiptResponseBoundsRemainEnforced(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	digest := strings.Repeat("e", 64)
	fronting := FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: "response-bound-fronting", RequestDigest: digest,
		Status: FrontingReceiptPending, ResponseJSON: []byte(`{}`), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&fronting).Error; err != nil {
		t.Fatal(err)
	}
	if err := New(db).CompleteFrontingReceiptV2(context.Background(), "apply", fronting.IdempotencyKey, digest, "receipt-response-fronting", 1, []byte(`{"value":"`+strings.Repeat("x", 64<<10)+`"}`), now); err == nil {
		t.Fatal("fronting accepted an oversized semantic response")
	}
	udp, _, err := New(db).BeginUDPGuardReceipt(context.Background(), "apply", "response-bound-udp", digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(db).CompleteUDPGuardReceipt(context.Background(), udp.ID, "receipt-response-udp", 1, strings.Repeat("x", 32<<10)); err == nil {
		t.Fatal("UDP guard accepted an oversized semantic response")
	}
	local, _, err := New(db).BeginLocalProxyReceipt(context.Background(), "apply", "response-bound-local", digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(db).CompleteLocalProxyReceipt(context.Background(), local.ID, "receipt-response-local", 1, strings.Repeat("x", 64<<10)); err == nil {
		t.Fatal("local proxy accepted an oversized semantic response")
	}
}

func TestReceiptLegacyKeyPruneForcesExplicitNewStructuredKey(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	digest := strings.Repeat("f", 64)
	for _, family := range []string{receiptFamilyFronting, receiptFamilyUDPGuard, receiptFamilyLocalProxy} {
		insertReceiptReceiptFamily(t, db, family, "legacy-expired-key", digest, "COMPLETE", "receipt-legacy-terminal", now.Add(-time.Hour).Unix(), 2)
	}
	if _, err := New(db).PruneIdempotencyReceipts(context.Background(), 1, now.Unix(), 8<<20); err != nil {
		t.Fatal(err)
	}
	assertReceiptExpiredAcrossFamilies(t, New(db), "legacy-expired-key", digest)
	assertReceiptExpiredAcrossFamilies(t, New(db), "another-missing-legacy-key", digest)
	newKey := receiptReceiptKey(now, "replacement-key")
	if _, _, err := New(db).BeginUDPGuardReceipt(context.Background(), "apply", newKey, digest); err != nil {
		t.Fatalf("structured replacement key rejected: %v", err)
	}
}

func insertReceiptReceiptFamily(t *testing.T, db *gorm.DB, family, key, digest, status, operationID string, updatedAt int64, responseBytes int) {
	t.Helper()
	response := []byte(`{}`)
	if responseBytes > 2 {
		response = []byte(`{"value":"` + strings.Repeat("x", responseBytes) + `"}`)
	}
	revision := 0
	if operationID != "" {
		revision = 1
	}
	switch family {
	case receiptFamilyFronting:
		row := FrontingIdempotencyV2Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest, OperationID: operationID,
			OperationRevision: revision, Status: status, ResponseJSON: response, CreatedAt: updatedAt, UpdatedAt: updatedAt}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	case receiptFamilyUDPGuard:
		row := UDPGuardIdempotencyV1Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest, OperationID: operationID,
			OperationRevision: revision, Status: status, SemanticResponseJSON: response, CreatedAt: updatedAt, UpdatedAt: updatedAt}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	case receiptFamilyLocalProxy:
		row := LocalProxyIdempotencyV1Model{Action: "apply", IdempotencyKey: key, RequestDigest: digest, OperationID: operationID,
			OperationRevision: revision, Status: status, SemanticResponseJSON: response, CreatedAt: updatedAt, UpdatedAt: updatedAt}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown receipt family %q", family)
	}
}

func assertReceiptExpiredAcrossFamilies(t *testing.T, repository *Repository, key, digest string) {
	t.Helper()
	if _, err := repository.FrontingReceiptV2(context.Background(), "apply", key); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("fronting expired key error=%v", err)
	}
	if _, _, err := repository.BeginUDPGuardReceipt(context.Background(), "apply", key, digest); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("UDP expired key error=%v", err)
	}
	if _, _, err := repository.ReplayLocalProxyReceipt(context.Background(), "apply", key, digest); !errors.Is(err, ErrIdempotencyKeyExpired) {
		t.Fatalf("local-proxy expired key error=%v", err)
	}
}

func receiptReceiptKey(at time.Time, suffix string) string {
	return fmt.Sprintf("%s%010d.%s", receiptKeyV1Prefix, at.UTC().Unix(), suffix)
}
