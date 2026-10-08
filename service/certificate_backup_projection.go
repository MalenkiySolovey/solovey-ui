package service

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"gorm.io/gorm"
)

// projectCertificateRuntimeFiles is an ephemeral offline build view. Only the
// certificate owner's verified logical keys authorize path-to-text projection;
// neither the archive nor arbitrary migration JSON supplies host paths to read.
func projectCertificateRuntimeFiles(ctx context.Context, db *gorm.DB, files []backup.OwnerFile, config []byte) ([]byte, error) {
	refs, err := certificateReferences(ctx, db)
	if err != nil {
		return config, err
	}
	byPath := map[string]string{}
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasPrefix(file.Key, "provider:") {
			continue
		}
		path, ok := refs[file.Key]
		if !ok || seen[file.Key] {
			return config, errors.New("TLS_FILE_PROJECTION_INVENTORY_INVALID")
		}
		seen[file.Key] = true
		if block, _ := pem.Decode(file.Data); block == nil {
			return config, errors.New("TLS_FILE_PROJECTION_PEM_INVALID")
		}
		if previous, exists := byPath[path]; exists && previous != string(file.Data) {
			return config, errors.New("TLS_FILE_PROJECTION_PATH_CONFLICT")
		}
		byPath[path] = string(file.Data)
	}
	if len(seen) != len(refs) {
		return config, errors.New("TLS_FILE_PROJECTION_INVENTORY_MISSING")
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) != nil {
		return config, errors.New("TLS_FILE_PROJECTION_CONFIG_INVALID")
	}
	projectConsumer := func(raw json.RawMessage) (json.RawMessage, error) {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return raw, nil
		}
		if tls, exists := object["tls"]; exists {
			var fields map[string]json.RawMessage
			if json.Unmarshal(tls, &fields) == nil && fields != nil {
				for _, pair := range [][2]string{{"certificate_path", "certificate"}, {"key_path", "key"}, {"client_certificate_path", "client_certificate"}, {"client_key_path", "client_key"}} {
					value, exists := fields[pair[0]]
					if !exists {
						continue
					}
					var scalar string
					var paths []string
					list := json.Unmarshal(value, &scalar) != nil
					if list {
						if json.Unmarshal(value, &paths) != nil {
							return nil, errors.New("TLS_FILE_PROJECTION_PATH_SHAPE_INVALID")
						}
					} else {
						paths = []string{scalar}
					}
					texts := []string{}
					complete := true
					for _, path := range paths {
						text, ok := byPath[path]
						if !ok {
							complete = false
							break
						}
						texts = append(texts, text)
					}
					if !complete || len(texts) == 0 {
						continue
					}
					if list {
						fields[pair[1]], _ = json.Marshal(texts)
					} else {
						fields[pair[1]], _ = json.Marshal(texts[0])
					}
					delete(fields, pair[0])
				}
				object["tls"], _ = json.Marshal(fields)
			}
		}
		return json.Marshal(object)
	}
	// Each listed section contains consumers whose direct TLS field is part of
	// the pinned schema. Nested custom data and other sections are not traversed.
	for _, section := range []string{"inbounds", "outbounds", "endpoints", "services", "http_clients"} {
		if raw, exists := root[section]; exists {
			var consumers []json.RawMessage
			if json.Unmarshal(raw, &consumers) != nil {
				return config, errors.New("TLS_FILE_PROJECTION_SECTION_INVALID")
			}
			for i, consumer := range consumers {
				projected, err := projectConsumer(consumer)
				if err != nil {
					return config, err
				}
				consumers[i] = projected
			}
			root[section], _ = json.Marshal(consumers)
		}
	}
	return json.Marshal(root)
}
