package service

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
	"gorm.io/gorm"
)

func init() {
	backup.RegisterFileOwner("certificates", backup.FileOwner{Export: exportCertificateFiles, Restore: restoreCertificateFiles})
}

// The certificate owner recognizes only its setting keys and TLS path fields.
// An archive can never authorize arbitrary filesystem paths for restoration.
var certificateSettingKeys = []string{"webCertFile", "webKeyFile", "ipCertCertPath", "ipCertKeyPath"}

func certificateReferences(ctx context.Context, db *gorm.DB) (map[string]string, error) {
	result := map[string]string{}
	var settings []model.Setting
	if err := db.WithContext(ctx).Where("key IN ?", certificateSettingKeys).Find(&settings).Error; err != nil {
		return nil, err
	}
	for _, s := range settings {
		if s.Value != "" {
			result["setting:"+s.Key] = s.Value
		}
	}
	var profiles []model.Tls
	if err := db.WithContext(ctx).Limit(backup.MaxOwnerFiles + 1).Find(&profiles).Error; err != nil {
		return nil, err
	}
	if len(profiles) > backup.MaxOwnerFiles {
		return nil, errors.New("too many certificate profiles")
	}
	for _, profile := range profiles {
		for side, raw := range map[string]json.RawMessage{"server": profile.Server, "client": profile.Client} {
			if len(raw) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return nil, err
			}
			for _, field := range []string{"certificate_path", "key_path"} {
				var name string
				if len(fields[field]) == 0 {
					continue
				}
				if err := json.Unmarshal(fields[field], &name); err != nil {
					return nil, err
				}
				if name != "" {
					result[fmt.Sprintf("tls:%d:%s:%s", profile.Id, side, field)] = name
				}
			}
		}
	}
	return result, nil
}

func exportCertificateFiles(ctx context.Context, db *gorm.DB) ([]backup.OwnerFile, error) {
	refs, err := certificateReferences(ctx, db)
	if err != nil {
		return nil, err
	}
	result := []backup.OwnerFile{}
	total := 0
	for key, name := range refs {
		if !filepath.IsAbs(name) {
			return nil, errors.New("certificate backup requires resolved absolute file paths")
		}
		data, err := backup.ReadOwnedFile(filepath.Dir(name), name)
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > backup.MaxOwnerFilesBytes {
			return nil, errors.New("certificate backup exceeds bound")
		}
		if block, _ := pem.Decode(data); block == nil {
			return nil, errors.New("certificate file is not PEM")
		}
		result = append(result, backup.OwnerFile{Key: key, Data: data})
	}
	return result, nil
}

func restoreCertificateFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	refs, err := certificateReferences(ctx, db)
	if err != nil {
		return err
	}
	if len(refs) != len(files) {
		return errors.New("certificate backup inventory is incomplete")
	}
	seen := map[string]bool{}
	for _, f := range files {
		if _, ok := refs[f.Key]; !ok || seen[f.Key] {
			return errors.New("unknown certificate logical key")
		}
		seen[f.Key] = true
		if block, _ := pem.Decode(f.Data); block == nil {
			return errors.New("restored certificate file is not PEM")
		}
		name, err := backup.PublishOwnedFile(filepath.Join(ipcert.ManagedCertDir(), "restored"), f.Data, publish)
		if err != nil {
			return err
		}
		if key, ok := strings.CutPrefix(f.Key, "setting:"); ok {
			if err := db.WithContext(ctx).Model(&model.Setting{}).Where("key = ?", key).Update("value", name).Error; err != nil {
				return err
			}
			continue
		}
		parts := strings.Split(f.Key, ":")
		if len(parts) != 4 {
			return errors.New("invalid certificate key")
		}
		id, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return err
		}
		var profile model.Tls
		if err := db.WithContext(ctx).First(&profile, id).Error; err != nil {
			return err
		}
		raw := profile.Server
		if parts[2] == "client" {
			raw = profile.Client
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		fields[parts[3]], _ = json.Marshal(name)
		updated, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if err := db.WithContext(ctx).Model(&model.Tls{}).Where("id = ?", id).Update(parts[2], string(updated)).Error; err != nil {
			return err
		}
	}
	return nil
}
