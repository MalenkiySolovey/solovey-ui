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
	backup.RegisterFileOwner("certificates", backup.FileOwner{Export: exportCertificateFiles, Restore: restoreCertificateFiles, ProjectRuntime: projectCertificateRuntimeFiles})
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
			paths := []string{"certificate_path", "key_path", "client_certificate_path"}
			if side == "client" {
				paths = append(paths, "client_key_path")
			}
			for _, field := range paths {
				if len(fields[field]) == 0 {
					continue
				}
				var name string
				if json.Unmarshal(fields[field], &name) == nil {
					if name != "" {
						result[fmt.Sprintf("tls:%d:%s:%s", profile.Id, side, field)] = name
					}
				} else if side == "server" && field == "client_certificate_path" {
					var names []string
					if json.Unmarshal(fields[field], &names) != nil || len(names) > backup.MaxOwnerFiles {
						return nil, errors.New("server client CA file list is invalid or exceeds its bound")
					}
					for index, name := range names {
						if name != "" {
							result[fmt.Sprintf("tls:%d:%s:%s:%d", profile.Id, side, field, index)] = name
						}
					}
				} else {
					return nil, errors.New("certificate path has an invalid consumer shape")
				}
				if len(result) > backup.MaxOwnerFiles {
					return nil, errors.New("certificate inventory exceeds its bound")
				}
			}
		}
	}
	inline, err := inlineClientCertificateReferences(ctx, db)
	if err != nil {
		return nil, err
	}
	for key, path := range inline {
		result[key] = path
	}
	if len(result) > backup.MaxOwnerFiles {
		return nil, errors.New("certificate inventory exceeds its bound")
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
	providerFiles, err := exportProviderCertificateFiles(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(result)+len(providerFiles) > backup.MaxOwnerFiles {
		return nil, errors.New("certificate inventory exceeds its bound")
	}
	for _, file := range providerFiles {
		total += len(file.Data)
	}
	if total > backup.MaxOwnerFilesBytes {
		return nil, errors.New("certificate backup exceeds bound")
	}
	return append(result, providerFiles...), nil
}

func restoreCertificateFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	return db.WithContext(ctx).Transaction(func(candidate *gorm.DB) error {
		return restoreCertificateFilesCandidate(ctx, candidate, files, publish)
	})
}

func restoreCertificateFilesCandidate(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	var providerFiles, credentialFiles []backup.OwnerFile
	for _, file := range files {
		if strings.HasPrefix(file.Key, "provider:") {
			providerFiles = append(providerFiles, file)
		} else {
			credentialFiles = append(credentialFiles, file)
		}
	}
	files = credentialFiles
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
	}
	if err := restoreProviderCertificateFiles(ctx, db, providerFiles, publish); err != nil {
		return err
	}
	for _, f := range files {
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
		if parts[0] == "outbound" || parts[0] == "http-client" {
			if err := rebindInlineClientCertificate(ctx, db, parts, name); err != nil {
				return err
			}
			continue
		}
		if len(parts) != 4 && len(parts) != 5 {
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
		if len(parts) == 5 {
			var paths []string
			index, err := strconv.Atoi(parts[4])
			if err != nil || json.Unmarshal(fields[parts[3]], &paths) != nil || index < 0 || index >= len(paths) {
				return errors.New("invalid indexed certificate key")
			}
			paths[index] = name
			fields[parts[3]], _ = json.Marshal(paths)
		} else {
			fields[parts[3]], _ = json.Marshal(name)
		}
		updated, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if err := db.WithContext(ctx).Model(&model.Tls{}).Where("id = ?", id).Update(parts[2], json.RawMessage(updated)).Error; err != nil {
			return err
		}
	}
	return nil
}
