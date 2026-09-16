package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"gorm.io/gorm"
)

const (
	LogicalFileBackupEnv      = "SUI_LOGICAL_FILE_BACKUP"
	LogicalFileBackupRequired = "OWNER_FILES_V1"
	BackupFileTable           = "backup_owner_files_v1"
	MaxOwnerFiles             = 4096
	MaxOwnerFileBytes         = 16 << 20
	MaxOwnerFilesBytes        = 128 << 20
)

// OwnerFile contains a semantic owner key, never an archive-authorized output
// path. Only the registered owner may interpret the key and publish its bytes.
type OwnerFile struct {
	Owner  string `gorm:"primaryKey"`
	Key    string `gorm:"primaryKey"`
	Data   []byte `gorm:"not null"`
	Digest string `gorm:"not null"`
}

func (OwnerFile) TableName() string { return BackupFileTable }

// FileOwner retains interpretation and publication at the semantic owner.
// Restore with publish=false may change only its staged DB, never live files.
// Published files must be immutable so DB rollback cannot alter old live data.
type FileOwner struct {
	Component string
	Export    func(context.Context, *gorm.DB) ([]OwnerFile, error)
	Restore   func(context.Context, *gorm.DB, []OwnerFile, bool) error
}

var fileOwners = struct {
	sync.RWMutex
	items map[string]FileOwner
}{items: map[string]FileOwner{}}

func RegisterFileOwner(id string, owner FileOwner) {
	if !safeOwnerName(id) || owner.Export == nil || owner.Restore == nil {
		panic("invalid logical file owner")
	}
	fileOwners.Lock()
	defer fileOwners.Unlock()
	if _, exists := fileOwners.items[id]; exists || len(fileOwners.items) >= 64 {
		panic("duplicate or excessive logical file owners")
	}
	fileOwners.items[id] = owner
}

func logicalFileBackupRequired() (bool, error) {
	switch os.Getenv(LogicalFileBackupEnv) {
	case "":
		return false, nil
	case LogicalFileBackupRequired:
		return true, nil
	default:
		return false, errors.New("logical file backup capability is invalid")
	}
}

func selectedFileOwners() (map[string]FileOwner, error) {
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	components := map[string]bool{}
	for _, c := range installed {
		components[c.ID] = true
	}
	fileOwners.RLock()
	defer fileOwners.RUnlock()
	result := map[string]FileOwner{}
	for id, owner := range fileOwners.items {
		if owner.Component == "" || components[owner.Component] {
			result[id] = owner
		}
	}
	return result, nil
}

type FileBackupManifest struct {
	Schema        string   `json:"schema"`
	Owners        []string `json:"owners"`
	Rows          int64    `json:"rows"`
	SchemaDigest  string   `json:"schemaDigest"`
	ContentDigest string   `json:"contentDigest"`
}

func exportOwnerFiles(ctx context.Context, source, destination *gorm.DB) (*FileBackupManifest, error) {
	required, err := logicalFileBackupRequired()
	if err != nil || !required {
		return nil, err
	}
	owners, err := selectedFileOwners()
	if err != nil || len(owners) == 0 {
		return nil, errors.Join(errors.New("logical file owners unavailable"), err)
	}
	if err := destination.AutoMigrate(&OwnerFile{}); err != nil {
		return nil, err
	}
	manifest := &FileBackupManifest{Schema: "solovey.logical-owner-files/v1"}
	for id := range owners {
		manifest.Owners = append(manifest.Owners, id)
	}
	sort.Strings(manifest.Owners)
	count, total := 0, int64(0)
	for _, id := range manifest.Owners {
		files, err := owners[id].Export(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("logical file owner %s: %w", id, err)
		}
		for _, f := range files {
			f.Owner = id
			sum := sha256.Sum256(f.Data)
			f.Digest = hex.EncodeToString(sum[:])
			count++
			total += int64(len(f.Data))
			if !validOwnerFile(f) || count > MaxOwnerFiles || total > MaxOwnerFilesBytes {
				return nil, errors.New("logical file bounds exceeded")
			}
			if err := destination.WithContext(ctx).Create(&f).Error; err != nil {
				return nil, err
			}
		}
	}
	digest, err := digestBackupTable(ctx, destination, "core", BackupFileTable)
	if err != nil {
		return nil, err
	}
	manifest.Rows, manifest.SchemaDigest, manifest.ContentDigest = digest.Rows, digest.SchemaDigest, digest.ContentDigest
	return manifest, nil
}

func validOwnerFile(f OwnerFile) bool {
	if !safeOwnerName(f.Owner) || f.Key == "" || len(f.Key) > 4096 || strings.ContainsAny(f.Key, "\x00\r\n") || len(f.Data) > MaxOwnerFileBytes {
		return false
	}
	sum := sha256.Sum256(f.Data)
	return f.Digest == hex.EncodeToString(sum[:])
}

func verifyOwnerFiles(ctx context.Context, db *gorm.DB, m *FileBackupManifest) error {
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
		if !safeOwnerName(id) || owners[id] {
			return errors.New("invalid logical file owner inventory")
		}
		owners[id] = true
	}
	digest, err := digestBackupTable(ctx, db, "core", BackupFileTable)
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
		if !owners[f.Owner] || !validOwnerFile(f) || total > MaxOwnerFilesBytes {
			return errors.New("invalid logical file payload")
		}
	}
	return nil
}

func restoreOwnerFiles(ctx context.Context, db *gorm.DB, m *FileBackupManifest, publish bool) error {
	required, err := logicalFileBackupRequired()
	if err != nil {
		return err
	}
	if m == nil {
		if required {
			return errors.New("selected deployment requires complete logical file backup")
		}
		return nil
	}
	if !required {
		return errors.New("logical file restore requires an explicitly selected deployment capability")
	}
	if err := verifyOwnerFiles(ctx, db, m); err != nil {
		return err
	}
	owners, err := selectedFileOwners()
	if err != nil {
		return err
	}
	if len(owners) != len(m.Owners) {
		return errors.New("logical file owner inventory changed")
	}
	for _, id := range m.Owners {
		owner, ok := owners[id]
		if !ok {
			return errors.New("logical file owner unavailable")
		}
		var files []OwnerFile
		if err := db.WithContext(ctx).Where("owner = ?", id).Order("key").Limit(MaxOwnerFiles + 1).Find(&files).Error; err != nil {
			return err
		}
		if err := owner.Restore(ctx, db, files, publish); err != nil {
			return fmt.Errorf("restore logical file owner %s: %w", id, err)
		}
	}
	if publish {
		return db.WithContext(ctx).Migrator().DropTable(&OwnerFile{})
	}
	return nil
}
