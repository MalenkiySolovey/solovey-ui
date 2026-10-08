package service

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/ipcert"
	"gorm.io/gorm"
)

type providerFileManifest struct {
	Schema string   `json:"schema"`
	Keys   []string `json:"keys"`
}

func certificateProviderState(ctx context.Context, db *gorm.DB) (entitytls.ProviderUpgrade, []model.Tls, []entitytls.ProviderDefinition, error) {
	stored, err := entitytls.ReadProviderDefinitions(db)
	if err != nil {
		return entitytls.ProviderUpgrade{}, nil, nil, err
	}
	var profiles []model.Tls
	if err := db.WithContext(ctx).Limit(backup.MaxOwnerFiles + 1).Find(&profiles).Error; err != nil {
		return entitytls.ProviderUpgrade{}, nil, nil, err
	}
	if len(profiles) > backup.MaxOwnerFiles {
		return entitytls.ProviderUpgrade{}, nil, nil, errors.New("TLS_PROVIDER_FILE_INVENTORY_EXCESSIVE")
	}
	prepared, err := entitytls.PrepareProviderUpgrade(profiles, stored)
	return prepared, profiles, stored, err
}

func providerFilePayloadValid(key string, data []byte) bool {
	if len(data) > backup.MaxOwnerFileBytes {
		return false
	}
	if strings.HasSuffix(key, ".json") {
		return json.Valid(data)
	}
	block, _ := pem.Decode(data)
	return block != nil
}

func exportProviderCertificateFiles(ctx context.Context, db *gorm.DB) ([]backup.OwnerFile, error) {
	prepared, _, _, err := certificateProviderState(ctx, db)
	if err != nil {
		return nil, err
	}
	result := []backup.OwnerFile{}
	total := 0
	for _, definition := range prepared.Providers {
		inventory, err := entitytls.ProviderFileInventory(ctx, definition)
		if err != nil {
			return nil, err
		}
		// The pinned adapter may discover another account when email is absent,
		// or search by an explicit key. Do not guess that historical choice or
		// copy every account from shared host storage.
		if inventory.AccountDiscovery {
			folders, err := os.ReadDir(filepath.Join(inventory.Root, filepath.FromSlash(inventory.AccountPrefix)))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("TLS_PROVIDER_STORAGE_UNAVAILABLE")
			}
			for _, folder := range folders {
				if folder.Name() != inventory.AccountFolder {
					return nil, errors.New("TLS_PROVIDER_ACCOUNT_SELECTION_MANUAL: set the intended account email before portable backup")
				}
			}
		}
		manifest := providerFileManifest{Schema: "solovey.acme-owned-files/v1", Keys: []string{}}
		for _, group := range inventory.Groups {
			var present []backup.OwnerFile
			for _, key := range group {
				name := filepath.Join(inventory.Root, filepath.FromSlash(key))
				if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
					continue
				} else if err != nil {
					return nil, errors.New("TLS_PROVIDER_FILE_UNAVAILABLE")
				}
				data, err := backup.ReadOwnedFile(inventory.Root, name)
				if err != nil || !providerFilePayloadValid(key, data) {
					return nil, errors.New("TLS_PROVIDER_FILE_INVALID: required managed data is unavailable, indirect or invalid")
				}
				present = append(present, backup.OwnerFile{Key: "provider:" + definition.Tag + ":file:" + key, Data: data})
				manifest.Keys = append(manifest.Keys, key)
				total += len(data)
			}
			if len(present) != 0 && len(present) != len(group) {
				return nil, errors.New("TLS_PROVIDER_FILE_MISSING: required account pair or certificate inventory is incomplete")
			}
			result = append(result, present...)
		}
		sort.Strings(manifest.Keys)
		raw, _ := json.Marshal(manifest)
		result = append(result, backup.OwnerFile{Key: "provider:" + definition.Tag + ":manifest", Data: raw})
		total += len(raw)
		if len(result) > backup.MaxOwnerFiles || total > backup.MaxOwnerFilesBytes {
			return nil, errors.New("TLS_PROVIDER_FILE_INVENTORY_EXCESSIVE")
		}
	}
	return result, nil
}

func restoreProviderCertificateFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, publish bool) error {
	prepared, original, stored, err := certificateProviderState(ctx, db)
	if err != nil {
		return err
	}
	byKey := map[string][]byte{}
	for _, file := range files {
		if _, duplicate := byKey[file.Key]; duplicate {
			return errors.New("TLS_PROVIDER_FILE_INVENTORY_INVALID")
		}
		byKey[file.Key] = file.Data
	}
	rebound := map[string]entitytls.ProviderDefinition{}
	// Validate every provider inventory before creating any files or changing DB
	// rows. Archive keys are checked against the actual TLS owner configuration.
	trees := map[string]map[string][]byte{}
	for _, definition := range prepared.Providers {
		inventory, err := entitytls.ProviderFileInventory(ctx, definition)
		if err != nil {
			return err
		}
		prefix := "provider:" + definition.Tag + ":"
		var manifest providerFileManifest
		raw, present := byKey[prefix+"manifest"]
		if !present || json.Unmarshal(raw, &manifest) != nil || manifest.Schema != "solovey.acme-owned-files/v1" || len(manifest.Keys) > backup.MaxOwnerFiles {
			return errors.New("TLS_PROVIDER_FILE_MANIFEST_MISSING: restore requires a complete owned inventory")
		}
		delete(byKey, prefix+"manifest")
		allowed := map[string]bool{}
		for _, group := range inventory.Groups {
			for _, key := range group {
				allowed[key] = true
			}
		}
		tree := map[string][]byte{}
		for _, key := range manifest.Keys {
			_, duplicate := tree[key]
			data, present := byKey[prefix+"file:"+key]
			if !allowed[key] || duplicate || !present || !providerFilePayloadValid(key, data) {
				return errors.New("TLS_PROVIDER_FILE_INVENTORY_INVALID")
			}
			tree[key] = data
			delete(byKey, prefix+"file:"+key)
		}
		for _, group := range inventory.Groups {
			count := 0
			for _, key := range group {
				if _, present := tree[key]; present {
					count++
				}
			}
			if count != 0 && count != len(group) {
				return errors.New("TLS_PROVIDER_FILE_MISSING: required account pair or certificate inventory is incomplete")
			}
		}
		trees[definition.Tag] = tree
	}
	if len(byKey) != 0 {
		return errors.New("TLS_PROVIDER_FILE_INVENTORY_INVALID: unknown owned key")
	}
	for _, definition := range prepared.Providers {
		root, err := backup.PublishOwnedTree(filepath.Join(ipcert.ManagedCertDir(), "restored-acme", definition.Tag), trees[definition.Tag], publish)
		if err != nil {
			return errors.New("TLS_PROVIDER_FILE_PUBLISH_FAILED: owner destination is unavailable or conflicts with prior data")
		}
		definition, err = entitytls.RebindProviderDirectory(definition, root)
		if err != nil {
			return err
		}
		rebound[definition.Tag] = definition
	}
	for i, definition := range stored {
		stored[i] = rebound[definition.Tag]
	}
	if err := entitytls.WriteProviderDefinitions(db, stored); err != nil {
		return err
	}
	for index, profile := range original {
		var projected, fields map[string]json.RawMessage
		_ = json.Unmarshal(prepared.Profiles[index].Server, &projected)
		_ = json.Unmarshal(profile.Server, &fields)
		var tag string
		_ = json.Unmarshal(projected["certificate_provider"], &tag)
		definition, found := rebound[tag]
		if !found {
			continue
		}
		if _, legacy := fields["acme"]; legacy {
			fields["acme"] = definition.Options
		} else {
			var reference string
			if json.Unmarshal(fields["certificate_provider"], &reference) == nil {
				continue
			}
			var options map[string]json.RawMessage
			_ = json.Unmarshal(definition.Options, &options)
			options["type"], _ = json.Marshal(definition.Type)
			fields["certificate_provider"], _ = json.Marshal(options)
		}
		updated, _ := json.Marshal(fields)
		if err := db.WithContext(ctx).Model(&model.Tls{}).Where("id = ?", profile.Id).Update("server", json.RawMessage(updated)).Error; err != nil {
			return err
		}
	}
	return nil
}
