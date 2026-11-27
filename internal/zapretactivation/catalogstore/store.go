// Package catalogstore persists the Zapret catalog inside the existing
// configuration database. It does not parse or compile native strategies.
package catalogstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const Table = "zapret_catalog_v2"
const schema = "solovey.zapret-catalog/v1" // The public JSON and revision stay stable.

const (
	recordKind      = "strategy"
	listKind        = "list"
	blobKind        = "blob"
	luaDraftKind    = "lua"
	luaRevisionKind = "lua_revision"
)

var errCatalog = errors.New("invalid stored Zapret strategy catalog")

type row struct {
	Kind       string `gorm:"primaryKey;not null"`
	ID         string `gorm:"primaryKey;not null"`
	Payload    string `gorm:"not null"`
	Definition string `gorm:"not null"`
}

func (row) TableName() string { return Table }

type document struct {
	Schema       string                     `json:"schema"`
	Records      map[string]json.RawMessage `json:"records"`
	Lists        map[string]json.RawMessage `json:"lists,omitempty"`
	Blobs        map[string]json.RawMessage `json:"blobAssets,omitempty"`
	LuaDrafts    map[string]json.RawMessage `json:"luaDrafts,omitempty"`
	LuaRevisions map[string]json.RawMessage `json:"luaRevisions,omitempty"`
}

func parse(data json.RawMessage) (document, error) {
	if data == nil {
		return document{Schema: schema, Records: map[string]json.RawMessage{}}, nil
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil || doc.Schema != schema || doc.Records == nil || decoder.Decode(new(any)) != io.EOF {
		return document{}, errCatalog
	}
	return doc, nil
}

func Legacy(tx *gorm.DB) (json.RawMessage, error) {
	var setting model.Setting
	if err := tx.Select("key", "value").Where("key = ?", zapretactivation.CatalogKey).Limit(1).Find(&setting).Error; err != nil {
		return nil, err
	}
	if setting.Key == "" {
		return nil, nil
	}
	if !json.Valid([]byte(setting.Value)) {
		return nil, errCatalog
	}
	return json.RawMessage(setting.Value), nil
}

type reader struct {
	db     *gorm.DB
	legacy json.RawMessage
}

func open(tx *gorm.DB) (reader, error) {
	legacy, err := Legacy(tx)
	if err != nil {
		return reader{}, err
	}
	r := reader{legacy: legacy}
	if tx.Migrator().HasTable(&row{}) {
		r.db = tx
		if legacy != nil {
			var check row
			result := tx.Select("id").Limit(1).Find(&check)
			if result.Error != nil {
				return reader{}, result.Error
			}
			if result.RowsAffected != 0 {
				return reader{}, errCatalog
			}
		}
	}
	return r, nil
}

// Read assembles a complete document only for the editor and restore validation.
func Read(tx *gorm.DB) (json.RawMessage, error) {
	r, err := open(tx)
	if err != nil || r.legacy != nil || r.db == nil {
		return r.legacy, err
	}
	doc := document{Schema: schema, Records: map[string]json.RawMessage{}, Lists: map[string]json.RawMessage{},
		Blobs: map[string]json.RawMessage{}, LuaDrafts: map[string]json.RawMessage{}, LuaRevisions: map[string]json.RawMessage{}}
	rows, err := r.db.Model(&row{}).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item row
		if err := r.db.ScanRows(rows, &item); err != nil {
			return nil, err
		}
		payload := json.RawMessage(item.Payload)
		if !json.Valid(payload) || (item.Kind != recordKind && item.Definition != "") {
			return nil, errCatalog
		}
		switch item.Kind {
		case recordKind:
			var fields map[string]json.RawMessage
			if !json.Valid([]byte(item.Definition)) || json.Unmarshal(payload, &fields) != nil || fields == nil || fields["definition"] != nil {
				return nil, errCatalog
			}
			fields["definition"] = json.RawMessage(item.Definition)
			payload, err = json.Marshal(fields)
			if err != nil {
				return nil, err
			}
			doc.Records[item.ID] = payload
		case listKind:
			doc.Lists[item.ID] = payload
		case blobKind:
			doc.Blobs[item.ID] = payload
		case luaDraftKind:
			doc.LuaDrafts[item.ID] = payload
		case luaRevisionKind:
			doc.LuaRevisions[item.ID] = payload
		default:
			return nil, errCatalog
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// LoadSelected performs exact ID lookups. Raw editor text, inactive
// definitions and unused sources never enter this activation projection.
func LoadSelected(tx *gorm.DB, result *zapretactivation.Validation) error {
	r, err := open(tx)
	if err != nil {
		return err
	}
	legacy, err := parse(r.legacy)
	if err != nil {
		return err
	}
	for _, binding := range result.Bindings {
		id := binding.StrategyRef
		if !zapretactivation.ManagedStrategyRef(id) || result.Definitions[id] != nil {
			continue
		}
		definition, err := r.lookup(recordKind, id, true)
		if err != nil {
			return err
		}
		if r.legacy != nil && legacy.Records[id] != nil {
			var record struct{ Definition json.RawMessage }
			if err := json.Unmarshal(legacy.Records[id], &record); err != nil {
				return errCatalog
			}
			definition = record.Definition
		}
		var fields struct {
			ManagedLua []struct{ ID, Revision string }
		}
		if len(definition) == 0 || json.Unmarshal(definition, &fields) != nil {
			return fmt.Errorf("outbound %q: sealed Zapret strategy %q is missing or invalid", binding.OutboundRef, id)
		}
		if result.Definitions == nil {
			result.Definitions = make(map[string]json.RawMessage)
		}
		result.Definitions[id] = definition
		for _, ref := range fields.ManagedLua {
			if ref.ID == "" || strings.Contains(ref.ID, "/") || !zapretactivation.ValidDigest(ref.Revision) {
				return errCatalog
			}
			key := zapretactivation.LuaSourceKey(ref.ID, ref.Revision)
			if result.LuaSources[key] != nil {
				continue
			}
			var source json.RawMessage
			if r.legacy != nil {
				source = legacy.LuaRevisions[key]
				if source == nil {
					source = legacy.LuaDrafts[ref.ID]
				}
			} else {
				source, err = r.lookup(luaRevisionKind, key, false)
				if err == nil && source == nil {
					source, err = r.lookup(luaDraftKind, ref.ID, false)
				}
				if err != nil {
					return err
				}
			}
			if source == nil || !json.Valid(source) {
				return fmt.Errorf("outbound %q: selected Lua revision for %q is missing or invalid", binding.OutboundRef, ref.ID)
			}
			if result.LuaSources == nil {
				result.LuaSources = make(map[string]json.RawMessage)
			}
			result.LuaSources[key] = source
		}
	}
	return nil
}

func (r reader) lookup(kind, id string, definition bool) (json.RawMessage, error) {
	if r.db == nil || r.legacy != nil {
		return nil, nil
	}
	column := "payload"
	if definition {
		column = "definition"
	}
	var item row
	if err := r.db.Select(column).Where("kind = ? AND id = ?", kind, id).Limit(1).Find(&item).Error; err != nil {
		return nil, err
	}
	if definition {
		return json.RawMessage(item.Definition), nil
	}
	return json.RawMessage(item.Payload), nil
}

func (d document) entries() (map[[2]string]row, error) {
	rows := make(map[[2]string]row)
	for kind, entries := range map[string]map[string]json.RawMessage{recordKind: d.Records, listKind: d.Lists, blobKind: d.Blobs,
		luaDraftKind: d.LuaDrafts, luaRevisionKind: d.LuaRevisions} {
		for id, payload := range entries {
			item := row{Kind: kind, ID: id, Payload: string(payload)}
			if kind == recordKind {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(payload, &fields); err != nil || fields["definition"] == nil {
					return nil, errCatalog
				}
				item.Definition = string(fields["definition"])
				delete(fields, "definition")
				data, err := json.Marshal(fields)
				if err != nil {
					return nil, err
				}
				item.Payload = string(data)
			}
			rows[[2]string{kind, id}] = item
		}
	}
	return rows, nil
}

// Save changes only the affected rows in the caller's configuration transaction.
func Save(tx *gorm.DB, before, after json.RawMessage) error {
	if after == nil {
		return errCatalog
	}
	legacy, err := Legacy(tx)
	if err != nil {
		return err
	}
	if legacy != nil {
		return errors.New("migrate the Zapret catalog before editing it")
	}
	return replaceRows(tx, before, after)
}

func replaceRows(tx *gorm.DB, before, after json.RawMessage) error {
	oldDoc, err := parse(before)
	if err != nil {
		return err
	}
	newDoc, err := parse(after)
	if err != nil {
		return err
	}
	prior, err := oldDoc.entries()
	if err != nil {
		return err
	}
	next, err := newDoc.entries()
	if err != nil {
		return err
	}
	for key, item := range next {
		old := prior[key]
		delete(prior, key)
		if old == item {
			continue
		}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&item).Error; err != nil {
			return err
		}
	}
	for key := range prior {
		if err := tx.Where("kind = ? AND id = ?", key[0], key[1]).Delete(&row{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Migrate validates both historical and normalized data before accepting it.
func Migrate(db *gorm.DB, validate func(json.RawMessage) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&row{}); err != nil {
			return err
		}
		r, err := open(tx)
		if err != nil {
			return err
		}
		data, err := Read(tx)
		if err != nil {
			return err
		}
		if err := validate(data); err != nil {
			return err
		}
		if r.legacy == nil {
			return nil
		}
		if err := replaceRows(tx, nil, data); err != nil {
			return err
		}
		return tx.Where("key = ?", zapretactivation.CatalogKey).Delete(&model.Setting{}).Error
	})
}

func Drop(tx *gorm.DB) error {
	if err := tx.Migrator().DropTable(&row{}); err != nil {
		return err
	}
	return tx.Where("key = ?", zapretactivation.CatalogKey).Delete(&model.Setting{}).Error
}
