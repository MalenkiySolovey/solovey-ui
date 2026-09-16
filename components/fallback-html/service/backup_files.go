//go:build !minimal

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	domain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"gorm.io/gorm"
)

func init() {
	// Durable-owner registration is independent of enabled runtime hooks.
	backup.RegisterFileOwner("fallback-html", backup.FileOwner{Component: "fallback-html", Export: exportFallbackFiles, Restore: restoreFallbackFiles})
}

func exportFallbackFiles(ctx context.Context, db *gorm.DB) ([]backup.OwnerFile, error) {
	result := []backup.OwnerFile{}
	total := int64(0)
	appendFile := func(kind string, id uint, name, digest string, size int64) error {
		total += size
		if size < 0 || size > backup.MaxOwnerFileBytes || total > backup.MaxOwnerFilesBytes {
			return errors.New("fallback backup files exceed bound")
		}
		data, err := backup.ReadOwnedFile(storageRoot(), name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != digest || int64(len(data)) != size {
			return errors.New("fallback file differs from database identity")
		}
		result = append(result, backup.OwnerFile{Key: fmt.Sprintf("%s:%d", kind, id), Data: data})
		return nil
	}
	var assets []domain.Asset
	if db.Migrator().HasTable(&domain.Asset{}) {
		if err := db.WithContext(ctx).Limit(backup.MaxOwnerFiles + 1).Find(&assets).Error; err != nil {
			return nil, err
		}
		if len(assets) > backup.MaxOwnerFiles {
			return nil, errors.New("too many fallback assets")
		}
		for _, a := range assets {
			if err := appendFile("asset", a.ID, a.FilePath, a.Sha256, a.SizeBytes); err != nil {
				return nil, err
			}
		}
	}
	var files []domain.PublishFile
	if db.Migrator().HasTable(&domain.PublishFile{}) {
		if err := db.WithContext(ctx).Limit(backup.MaxOwnerFiles + 1).Find(&files).Error; err != nil {
			return nil, err
		}
		if len(assets)+len(files) > backup.MaxOwnerFiles {
			return nil, errors.New("too many fallback files")
		}
		for _, f := range files {
			if err := appendFile("publish", f.ID, f.FilePath, f.Sha256, f.SizeBytes); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func restoreFallbackFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	seen := map[string]bool{}
	for _, f := range files {
		kind, rawID, ok := strings.Cut(f.Key, ":")
		id, err := strconv.ParseUint(rawID, 10, 64)
		if !ok || err != nil || id == 0 || strconv.FormatUint(id, 10) != rawID || seen[f.Key] {
			return errors.New("invalid fallback logical file key")
		}
		seen[f.Key] = true
		var model any
		switch kind {
		case "asset":
			model = &domain.Asset{}
		case "publish":
			model = &domain.PublishFile{}
		default:
			return errors.New("unknown fallback file kind")
		}
		var row struct {
			Sha256    string
			SizeBytes int64
		}
		query := db.WithContext(ctx).Model(model).Where("id = ?", id).Select("sha256, size_bytes").Take(&row)
		if query.Error != nil || row.Sha256 != f.Digest || row.SizeBytes != int64(len(f.Data)) {
			return errors.New("fallback file does not match owner row")
		}
		root := filepath.Join(storageRoot(), "restored")
		if kind == "asset" {
			var asset domain.Asset
			if err := db.WithContext(ctx).First(&asset, id).Error; err != nil {
				return err
			}
			root = filepath.Join(assetRoot(asset.SiteID), "restored")
		}
		name, err := backup.PublishOwnedFile(root, f.Data, publish)
		if err != nil {
			return err
		}
		if err := db.WithContext(ctx).Model(model).Where("id = ?", id).Update("file_path", name).Error; err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		kind  string
		model any
	}{{"asset", &domain.Asset{}}, {"publish", &domain.PublishFile{}}} {
		if !db.Migrator().HasTable(entry.model) {
			continue
		}
		var ids []uint
		if err := db.WithContext(ctx).Model(entry.model).Limit(backup.MaxOwnerFiles+1).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > backup.MaxOwnerFiles {
			return errors.New("fallback file inventory exceeds bound")
		}
		for _, id := range ids {
			if !seen[fmt.Sprintf("%s:%d", entry.kind, id)] {
				return errors.New("fallback durable file missing from backup")
			}
		}
	}
	// Existing restore normalization deactivates publications and requires a
	// fresh verified publish; preserved assets allow that publish on a new host.
	return nil
}
