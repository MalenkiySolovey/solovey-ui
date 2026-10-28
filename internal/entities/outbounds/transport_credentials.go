package outbounds

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

const downloadClientKeyField = "transport.download.tls.client_key"

// The split connection remains part of Outbound.Options. Only this fixed
// credential path is resolved; request input cannot select a SecretRef path.
type downloadTLSOptions struct {
	root, transport, download, tls map[string]json.RawMessage
}

func readDownloadTLS(raw []byte) (*downloadTLSOptions, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return nil, errOutboundCredentialInvalid
	}
	// Mieru owns a string transport enum. Only the native XHTTP object
	// participates in this credential path.
	var transport map[string]json.RawMessage
	var kind string
	if json.Unmarshal(root["transport"], &transport) != nil || json.Unmarshal(transport["type"], &kind) != nil || kind != "xhttp" {
		return nil, nil
	}
	result := &downloadTLSOptions{root: root}
	next := root
	for index, name := range []string{"transport", "download", "tls"} {
		for key := range next {
			if key != name && strings.EqualFold(key, name) {
				return nil, errOutboundCredentialInvalid
			}
		}
		value, present := next[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, nil
		}
		var fields map[string]json.RawMessage
		if jsonvalue.StrictObject(value) != nil || json.Unmarshal(value, &fields) != nil || fields == nil {
			return nil, errOutboundCredentialInvalid
		}
		if index == 0 {
			var kind string
			_ = json.Unmarshal(fields["type"], &kind)
			if kind != "xhttp" {
				return nil, nil
			}
			result.transport = fields
		} else if index == 1 {
			result.download = fields
		} else {
			result.tls = fields
		}
		next = fields
	}
	for key := range result.tls {
		if key != "client_key" && strings.EqualFold(key, "client_key") {
			return nil, errOutboundCredentialInvalid
		}
	}
	return result, nil
}

func (o *downloadTLSOptions) marshal() ([]byte, error) {
	var err error
	o.download["tls"], err = json.Marshal(o.tls)
	if err != nil {
		return nil, err
	}
	o.transport["download"], err = json.Marshal(o.download)
	if err != nil {
		return nil, err
	}
	o.root["transport"], err = json.Marshal(o.transport)
	if err != nil {
		return nil, err
	}
	return json.Marshal(o.root)
}

func prepareTransportCredentials(tx *gorm.DB, row *model.Outbound) error {
	next, err := readDownloadTLS(row.Options)
	if err != nil || next == nil {
		return err
	}
	var oldRaw json.RawMessage
	var captured string
	if row.Id != 0 {
		var stored model.Outbound
		if err := tx.Select("id", "type", "options").First(&stored, row.Id).Error; err != nil {
			return err
		}
		if stored.Type == row.Type {
			old, err := readDownloadTLS(stored.Options)
			if err != nil {
				return err
			}
			if old != nil {
				oldRaw, captured = capturedOutboundCredential(old.tls, "client_key")
			}
		}
	}
	mutation, supplied := next.tls["client_key"]
	if !supplied {
		if len(oldRaw) != 0 {
			next.tls["client_key"] = oldRaw
		}
	} else if len(oldRaw) == 0 || !bytes.Equal(mutation, oldRaw) {
		var intent struct{ Intent, Value string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil {
			return errOutboundCredentialInvalid
		}
		if row.Id == 0 {
			if intent.Intent == "clear" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
					return errOutboundCredentialInvalid
				}
				delete(next.tls, "client_key")
				row.Options, err = next.marshal()
				return err
			}
			if intent.Intent != "replace" {
				return errOutboundCredentialInvalid
			}
			placeholder := *row
			placeholder.Options = json.RawMessage("{}")
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Outbound{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			row.Id = placeholder.Id
		}
		ref, err := ownerdb.Reference("outbounds", row.Id, downloadClientKeyField)
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		if sealed == "" {
			delete(next.tls, "client_key")
		} else if sealed == captured && len(oldRaw) != 0 {
			next.tls["client_key"] = oldRaw
		} else {
			next.tls["client_key"], _ = json.Marshal(sealed)
		}
	}
	row.Options, err = next.marshal()
	return err
}

func projectTransportCredentials(row model.Outbound, data *map[string]interface{}) error {
	raw, err := json.Marshal(*data)
	if err != nil {
		return err
	}
	options, err := readDownloadTLS(raw)
	if err != nil || options == nil {
		return err
	}
	_, captured := capturedOutboundCredential(options.tls, "client_key")
	ref, err := ownerdb.Reference("outbounds", row.Id, downloadClientKeyField)
	if err != nil {
		return err
	}
	options.tls["client_key"], err = json.Marshal(ownerdb.View(ref, captured))
	if err != nil {
		return err
	}
	projected, err := options.marshal()
	if err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(projected, &root) != nil {
		return errOutboundCredentialInvalid
	}
	(*data)["transport"] = root["transport"]
	return nil
}

func marshalTransportRuntime(db *gorm.DB, row model.Outbound, raw []byte) ([]byte, error) {
	options, err := readDownloadTLS(raw)
	if err != nil || options == nil {
		return raw, err
	}
	_, captured := capturedOutboundCredential(options.tls, "client_key")
	if captured == "" {
		delete(options.tls, "client_key")
		return options.marshal()
	}
	if !secretbox.IsEncrypted(captured) {
		return nil, errOutboundCredentialInvalid
	}
	ref, err := ownerdb.Reference("outbounds", row.Id, downloadClientKeyField)
	if err != nil {
		return nil, err
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "outbounds"}, Operation: secretref.UseForOwner, IntentID: "outbound-transport-config"}
	_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
		options.tls["client_key"], _ = json.Marshal(string(value))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return options.marshal()
}

func validateTransportReferences(db *gorm.DB) error {
	var rows []model.Outbound
	if !db.Migrator().HasTable(&model.Outbound{}) {
		return nil
	}
	if err := db.Select("id", "type", "tag", "options").Find(&rows).Error; err != nil {
		return err
	}
	var endpoints []model.Endpoint
	if db.Migrator().HasTable(&model.Endpoint{}) {
		if err := db.Select("id", "type", "tag", "options").Find(&endpoints).Error; err != nil {
			return err
		}
	}
	return tagrefs.ValidateTransportDownloadReferences(rows, endpoints)
}
