package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

var inlineClientCertificateFields = []string{"certificate_path", "client_certificate_path", "client_key_path"}

// These adapters enumerate actual stored TLS consumers. They never recurse
// arbitrary JSON, treat migration data as a host path, or own file publication.
func inlineClientCertificateReferences(ctx context.Context, db *gorm.DB) (map[string]string, error) {
	result := map[string]string{}
	collect := func(prefix string, raw json.RawMessage) error {
		var options map[string]json.RawMessage
		if json.Unmarshal(raw, &options) != nil {
			return errors.New("TLS_INLINE_CLIENT_OPTIONS_INVALID")
		}
		var tls map[string]json.RawMessage
		if field, present := options["tls"]; present && json.Unmarshal(field, &tls) != nil {
			return errors.New("TLS_INLINE_CLIENT_OPTIONS_INVALID")
		}
		for _, key := range inlineClientCertificateFields {
			if value, present := tls[key]; present {
				var path string
				if json.Unmarshal(value, &path) != nil {
					return errors.New("TLS_INLINE_CLIENT_PATH_INVALID")
				}
				if path != "" {
					result[prefix+":tls:"+key] = path
				}
			}
		}
		return nil
	}
	if db.Migrator().HasTable(&model.Outbound{}) {
		var rows []model.Outbound
		if err := db.WithContext(ctx).Limit(backup.MaxOwnerFiles + 1).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) > backup.MaxOwnerFiles {
			return nil, errors.New("TLS_INLINE_CLIENT_INVENTORY_EXCEEDED")
		}
		for _, row := range rows {
			if len(row.Options) > 0 && string(row.Options) != "null" {
				if err := collect(fmt.Sprintf("outbound:%d", row.Id), row.Options); err != nil {
					return nil, err
				}
			}
		}
	}
	var setting model.Setting
	if err := db.WithContext(ctx).Where("key = ?", "config").Limit(1).Find(&setting).Error; err != nil {
		return nil, err
	}
	if setting.Value != "" {
		var root struct {
			Clients []json.RawMessage `json:"http_clients"`
		}
		if json.Unmarshal([]byte(setting.Value), &root) != nil {
			return nil, errors.New("TLS_INLINE_CLIENT_BASE_INVALID")
		}
		seen := map[string]bool{}
		for _, raw := range root.Clients {
			var header struct {
				Tag string `json:"tag"`
			}
			if json.Unmarshal(raw, &header) != nil || header.Tag == "" || seen[header.Tag] {
				return nil, errors.New("TLS_INLINE_CLIENT_IDENTITY_INVALID")
			}
			seen[header.Tag] = true
			if err := collect("http-client:"+base64.RawURLEncoding.EncodeToString([]byte(header.Tag)), raw); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func rebindInlineClientCertificate(ctx context.Context, db *gorm.DB, parts []string, name string) error {
	if len(parts) != 4 || parts[2] != "tls" {
		return errors.New("TLS_INLINE_CLIENT_KEY_INVALID")
	}
	valid := false
	for _, key := range inlineClientCertificateFields {
		valid = valid || key == parts[3]
	}
	if !valid {
		return errors.New("TLS_INLINE_CLIENT_KEY_INVALID")
	}
	rebind := func(raw json.RawMessage) (json.RawMessage, error) {
		var options, tls map[string]json.RawMessage
		if json.Unmarshal(raw, &options) != nil || json.Unmarshal(options["tls"], &tls) != nil || tls == nil {
			return nil, errors.New("TLS_INLINE_CLIENT_OPTIONS_INVALID")
		}
		tls[parts[3]], _ = json.Marshal(name)
		options["tls"], _ = json.Marshal(tls)
		return json.Marshal(options)
	}
	if parts[0] == "outbound" {
		id, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return errors.New("TLS_INLINE_CLIENT_KEY_INVALID")
		}
		var row model.Outbound
		if err := db.WithContext(ctx).First(&row, id).Error; err != nil {
			return err
		}
		updated, err := rebind(row.Options)
		if err != nil {
			return err
		}
		return db.WithContext(ctx).Model(&model.Outbound{}).Where("id = ?", id).Update("options", updated).Error
	}
	tag, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("TLS_INLINE_CLIENT_KEY_INVALID")
	}
	var setting model.Setting
	if err := db.WithContext(ctx).Where("key = ?", "config").First(&setting).Error; err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(setting.Value), &root) != nil {
		return errors.New("TLS_INLINE_CLIENT_BASE_INVALID")
	}
	var rows []json.RawMessage
	if json.Unmarshal(root["http_clients"], &rows) != nil {
		return errors.New("TLS_INLINE_CLIENT_BASE_INVALID")
	}
	found := false
	for i, raw := range rows {
		var header struct {
			Tag string `json:"tag"`
		}
		_ = json.Unmarshal(raw, &header)
		if header.Tag == string(tag) {
			if found {
				return errors.New("TLS_INLINE_CLIENT_IDENTITY_INVALID")
			}
			found = true
			rows[i], err = rebind(raw)
			if err != nil {
				return err
			}
		}
	}
	if !found {
		return errors.New("TLS_INLINE_CLIENT_IDENTITY_MISSING")
	}
	root["http_clients"], _ = json.Marshal(rows)
	updated, err := json.Marshal(root)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&model.Setting{}).Where("key = ?", "config").Update("value", string(updated)).Error
}
