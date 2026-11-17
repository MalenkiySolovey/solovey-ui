package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"gorm.io/gorm"
	"strings"
)

const BackupFileTable = "backup_owner_files_v1"

// OwnerFile contains an owner key and bounded immutable payload; it never authorizes an output path.
type OwnerFile struct {
	Owner  string `gorm:"primaryKey"`
	Key    string `gorm:"primaryKey"`
	Data   []byte `gorm:"not null"`
	Digest string `gorm:"not null"`
}

func (OwnerFile) TableName() string { return BackupFileTable }

func ValidOwnerFile(f OwnerFile) bool {
	if !ValidOwnerName(f.Owner) || f.Key == "" || len(f.Key) > 4096 || strings.ContainsAny(f.Key, "\x00\r\n") || len(f.Data) > MaxOwnerFileBytes {
		return false
	}
	sum := sha256.Sum256(f.Data)
	return f.Digest == hex.EncodeToString(sum[:])
}

func VerifyOwnerFiles(ctx context.Context, db *gorm.DB, m *FileBackupManifest) error {
	if m == nil {
		return nil
	}
	if m.Schema != "solovey.logical-owner-files/v1" || len(m.Owners) == 0 || len(m.Owners) > 64 || m.Rows < 0 || m.Rows > MaxOwnerFiles {
		return errors.New("invalid logical file manifest")
	}
	var bounds struct{ Rows, Bytes, Largest, KeyBytes, OwnerBytes int64 }
	if err := db.WithContext(ctx).Raw("SELECT COUNT(*) AS rows, COALESCE(SUM(length(data)),0) AS bytes, COALESCE(MAX(length(data)),0) AS largest, COALESCE(MAX(length(key)),0) AS key_bytes, COALESCE(MAX(length(owner)),0) AS owner_bytes FROM " + BackupFileTable).Scan(&bounds).Error; err != nil {
		return err
	}
	if bounds.Rows != m.Rows || bounds.Bytes > MaxOwnerFilesBytes || bounds.Largest > MaxOwnerFileBytes || bounds.KeyBytes > 4096 || bounds.OwnerBytes > 96 {
		return errors.New("logical file payload exceeds allocation bounds")
	}
	owners := map[string]bool{}
	for _, id := range m.Owners {
		if !ValidOwnerName(id) || owners[id] {
			return errors.New("invalid logical file owner inventory")
		}
		owners[id] = true
	}
	digest, err := DigestTable(ctx, db, "core", BackupFileTable)
	if err != nil || digest.Rows != m.Rows || digest.SchemaDigest != m.SchemaDigest || digest.ContentDigest != m.ContentDigest {
		return errors.New("logical file digest mismatch")
	}
	var files []OwnerFile
	if err := db.WithContext(ctx).Limit(MaxOwnerFiles + 1).Find(&files).Error; err != nil {
		return err
	}
	total := int64(0)
	for _, f := range files {
		total += int64(len(f.Data))
		if !owners[f.Owner] || !ValidOwnerFile(f) || total > MaxOwnerFilesBytes {
			return errors.New("invalid logical file payload")
		}
	}
	return nil
}
