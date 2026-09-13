package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	receiptFamilyFronting   = "fronting-v2"
	receiptFamilyUDPGuard   = "udp-guard-v1"
	receiptFamilyLocalProxy = "local-proxy-v1"
	receiptKeyV1Prefix      = "sp-receipt."
	receiptKeyFutureSkew    = int64(5 * 60)
)

var (
	ErrIdempotencyKeyExpired = errors.New("idempotency key is outside the retained replay horizon; use a new key")
	ErrIdempotencyKeyInvalid = errors.New("structured idempotency key is invalid")
)

type IdempotencyReceiptPruneResult struct {
	DeletedFrontingReceipts   int
	DeletedUDPGuardReceipts   int
	DeletedLocalProxyReceipts int
	DeletedBytes              int64
	Preserved                 int
}

type receiptRetentionCandidate struct {
	id               uint
	status           string
	key              string
	operationID      string
	createdAt        int64
	updatedAt        int64
	logicalBytes     int64
	structuredIssued int64
	structuredKey    bool
}

// PruneIdempotencyReceipts bounds the three independent semantic receipt
// families. PENDING is always live. AMBIGUOUS remains live until it is bound
// to an operation whose repaired shared closure is terminal and released.
// COMPLETE uses the same closure rule. Every delete and replay-fence advance
// commits in one SQLite transaction.
func (r *Repository) PruneIdempotencyReceipts(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (IdempotencyReceiptPruneResult, error) {
	if r == nil || r.db == nil {
		return IdempotencyReceiptPruneResult{}, errors.New("server-protection repository is not initialized")
	}
	if keepCount < 1 || cutoff <= 0 || keepBytes < 1 {
		return IdempotencyReceiptPruneResult{}, errors.New("idempotency receipt retention limits are invalid")
	}
	result := IdempotencyReceiptPruneResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		protected, err := protectedArtifactOperations(tx)
		if err != nil {
			return err
		}
		families := []struct {
			name    string
			table   string
			load    func(*gorm.DB) ([]receiptRetentionCandidate, error)
			deleted *int
		}{
			{receiptFamilyFronting, FrontingIdempotencyV2Model{}.TableName(), loadFrontingReceiptCandidates, &result.DeletedFrontingReceipts},
			{receiptFamilyUDPGuard, UDPGuardIdempotencyV1Model{}.TableName(), loadUDPGuardReceiptCandidates, &result.DeletedUDPGuardReceipts},
			{receiptFamilyLocalProxy, LocalProxyIdempotencyV1Model{}.TableName(), loadLocalProxyReceiptCandidates, &result.DeletedLocalProxyReceipts},
		}
		for _, family := range families {
			rows, err := family.load(tx)
			if err != nil {
				return err
			}
			keptCount := 0
			var keptBytes int64
			var expiredThrough int64
			legacyExpired := false
			for _, row := range rows {
				released, err := receiptOperationClosureReleased(tx, protected, row.operationID)
				if err != nil {
					return err
				}
				if !released {
					result.Preserved++
					continue
				}
				fitsBytes := row.logicalBytes >= 0 && row.logicalBytes <= keepBytes-keptBytes
				if keptCount < keepCount && row.updatedAt >= cutoff && fitsBytes {
					keptCount++
					keptBytes += row.logicalBytes
					result.Preserved++
					continue
				}
				deleted := tx.Table(family.table).Where("id = ? AND status = ? AND updated_at = ?", row.id, row.status, row.updatedAt).Delete(nil)
				if deleted.Error != nil {
					return deleted.Error
				}
				if deleted.RowsAffected != 1 {
					return fmt.Errorf("%s idempotency receipt retention fence changed for row %d", family.name, row.id)
				}
				*family.deleted++
				result.DeletedBytes = receiptSaturatingAdd(result.DeletedBytes, row.logicalBytes)
				if row.structuredKey {
					if row.structuredIssued > expiredThrough {
						expiredThrough = row.structuredIssued
					}
				} else {
					legacyExpired = true
				}
			}
			if expiredThrough > 0 || legacyExpired {
				if err := advanceReceiptFenceTx(tx, family.name, expiredThrough, legacyExpired, cutoff); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return result, err
}

func loadFrontingReceiptCandidates(tx *gorm.DB) ([]receiptRetentionCandidate, error) {
	var rows []FrontingIdempotencyV2Model
	if err := tx.Where("status IN ?", []string{FrontingReceiptComplete, FrontingReceiptAmbiguous}).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]receiptRetentionCandidate, 0, len(rows))
	for _, row := range rows {
		issued, structured := parseReceiptKeyV1(row.IdempotencyKey)
		result = append(result, receiptRetentionCandidate{id: row.ID, status: row.Status, key: row.IdempotencyKey,
			operationID: row.OperationID, createdAt: row.CreatedAt, updatedAt: row.UpdatedAt,
			logicalBytes:     int64(192 + len(row.Action) + len(row.IdempotencyKey) + len(row.RequestDigest) + len(row.OperationID) + len(row.ResponseJSON)),
			structuredIssued: issued, structuredKey: structured})
	}
	return result, nil
}

func loadUDPGuardReceiptCandidates(tx *gorm.DB) ([]receiptRetentionCandidate, error) {
	var rows []UDPGuardIdempotencyV1Model
	if err := tx.Where("status IN ?", []string{"COMPLETE", "AMBIGUOUS"}).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]receiptRetentionCandidate, 0, len(rows))
	for _, row := range rows {
		issued, structured := parseReceiptKeyV1(row.IdempotencyKey)
		result = append(result, receiptRetentionCandidate{id: row.ID, status: row.Status, key: row.IdempotencyKey,
			operationID: row.OperationID, createdAt: row.CreatedAt, updatedAt: row.UpdatedAt,
			logicalBytes:     int64(192 + len(row.Action) + len(row.IdempotencyKey) + len(row.RequestDigest) + len(row.OperationID) + len(row.SemanticResponseJSON)),
			structuredIssued: issued, structuredKey: structured})
	}
	return result, nil
}

func loadLocalProxyReceiptCandidates(tx *gorm.DB) ([]receiptRetentionCandidate, error) {
	var rows []LocalProxyIdempotencyV1Model
	if err := tx.Where("status IN ?", []string{"COMPLETE", "AMBIGUOUS"}).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]receiptRetentionCandidate, 0, len(rows))
	for _, row := range rows {
		issued, structured := parseReceiptKeyV1(row.IdempotencyKey)
		result = append(result, receiptRetentionCandidate{id: row.ID, status: row.Status, key: row.IdempotencyKey,
			operationID: row.OperationID, createdAt: row.CreatedAt, updatedAt: row.UpdatedAt,
			logicalBytes:     int64(192 + len(row.Action) + len(row.IdempotencyKey) + len(row.RequestDigest) + len(row.OperationID) + len(row.SemanticResponseJSON)),
			structuredIssued: issued, structuredKey: structured})
	}
	return result, nil
}

func receiptOperationClosureReleased(tx *gorm.DB, protected map[string]string, operationID string) (bool, error) {
	if operationID == "" {
		return false, nil
	}
	if _, live := protected[operationID]; live {
		return false, nil
	}
	var lock OperationLockModel
	query := tx.Where("operation_id = ?", operationID).Limit(1).Find(&lock)
	if query.Error != nil {
		return false, query.Error
	}
	if query.RowsAffected == 0 {
		return true, nil
	}
	return artifactContainsString(terminalFirewallOperationStates, lock.State), nil
}

func advanceReceiptFenceTx(tx *gorm.DB, family string, expiredThrough int64, legacyExpired bool, now int64) error {
	var current IdempotencyReplayFenceV1Model
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("family = ?", family).Limit(1).Find(&current)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return tx.Create(&IdempotencyReplayFenceV1Model{Family: family, ExpiredThrough: expiredThrough, LegacyExpired: legacyExpired, UpdatedAt: now}).Error
	}
	if current.ExpiredThrough > expiredThrough {
		expiredThrough = current.ExpiredThrough
	}
	legacyExpired = legacyExpired || current.LegacyExpired
	return tx.Model(&IdempotencyReplayFenceV1Model{}).Where("family = ?", family).
		Updates(map[string]any{"expired_through": expiredThrough, "legacy_expired": legacyExpired, "updated_at": now}).Error
}

func ensureReceiptKeyFreshTx(tx *gorm.DB, family, key string, now int64) error {
	issued, structured, malformed := inspectReceiptKeyV1(key)
	if malformed || structured && issued > now+receiptKeyFutureSkew {
		return ErrIdempotencyKeyInvalid
	}
	// Receipt claim paths can run while a rolling migration is still creating
	// the replay-fence table. Until that table exists there is no durable fence
	// to enforce, so retain the pre-Receipt missing-key behavior.
	if !tx.Migrator().HasTable(&IdempotencyReplayFenceV1Model{}) {
		return nil
	}
	var fence IdempotencyReplayFenceV1Model
	query := tx.Where("family = ?", family).Limit(1).Find(&fence)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return nil
	}
	if structured && issued <= fence.ExpiredThrough || !structured && fence.LegacyExpired {
		return ErrIdempotencyKeyExpired
	}
	return nil
}

func receiptLookupMissing(ctx context.Context, db *gorm.DB, family, key string, now int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return ensureReceiptKeyFreshTx(tx, family, key, now)
	})
}

func parseReceiptKeyV1(key string) (int64, bool) {
	issued, structured, malformed := inspectReceiptKeyV1(key)
	return issued, structured && !malformed
}

func inspectReceiptKeyV1(key string) (issued int64, structured bool, malformed bool) {
	if !strings.HasPrefix(key, receiptKeyV1Prefix) {
		return 0, false, false
	}
	structured = true
	rest := strings.TrimPrefix(key, receiptKeyV1Prefix)
	separator := strings.IndexByte(rest, '.')
	if separator < 1 || separator == len(rest)-1 {
		return 0, true, true
	}
	stamp, suffix := rest[:separator], rest[separator+1:]
	if len(stamp) != 10 || len(suffix) < 8 || len(suffix) > 80 {
		return 0, true, true
	}
	for _, char := range suffix {
		alphaNumeric := char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
		if !alphaNumeric && char != '-' && char != '_' {
			return 0, true, true
		}
	}
	value, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || value <= 0 {
		return 0, true, true
	}
	return value, true, false
}

func receiptSaturatingAdd(current, addition int64) int64 {
	if addition <= 0 {
		return current
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if current > maxInt64-addition {
		return maxInt64
	}
	return current + addition
}
