package outbounds

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	calloptions "github.com/MalenkiySolovey/solovey-ui/internal/entities/calloptions"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

// Native client credentials stay in Outbound.Options and use the common
// SecretRef lifecycle. Protocol selection creates no credential store.
var errOutboundCredentialInvalid = errors.New("OUTBOUND_CREDENTIAL_INVALID")

func managedCredentialFields(kind string) []string {
	switch kind {
	case "call":
		return []string{"cookies", "join_link"}
	case "mieru", "trusttunnel":
		return []string{"password"}
	case "sudoku":
		return []string{"key"}
	default:
		return nil
	}
}

func capturedOutboundCredential(options map[string]json.RawMessage, field string) (json.RawMessage, string) {
	raw := options[field]
	var captured string
	if len(raw) != 0 && json.Unmarshal(raw, &captured) != nil {
		captured = string(raw)
	}
	return raw, captured
}

func validateManagedCredential(kind, field, value string) error {
	if kind == "sudoku" && field == "key" {
		// The selected native source accepts split/private/public hex keys and
		// legacy PSK strings. Keep that language; all are private owner values.
		if !calloptions.CredentialValid("password", value) {
			return errOutboundCredentialInvalid
		}
		return nil
	}
	if kind != "call" && field != "password" {
		return errOutboundCredentialInvalid
	}
	if !calloptions.CredentialValid(field, value) {
		return errOutboundCredentialInvalid
	}
	return nil
}

func prepareManagedCredentials(tx *gorm.DB, outbound *model.Outbound) error {
	var next, old map[string]json.RawMessage
	if jsonvalue.StrictObject(outbound.Options) != nil || json.Unmarshal(outbound.Options, &next) != nil || next == nil {
		return errOutboundCredentialInvalid
	}
	if outbound.Id != 0 {
		var stored model.Outbound
		if err := tx.Select("id", "type", "options").First(&stored, outbound.Id).Error; err != nil {
			return err
		}
		if stored.Type == outbound.Type && len(stored.Options) != 0 && json.Unmarshal(stored.Options, &old) != nil {
			return errOutboundCredentialInvalid
		}
	}
	available := entitycapabilities.Resolve("outbounds", outbound.Type).Available
	for _, field := range managedCredentialFields(outbound.Type) {
		oldRaw, captured := capturedOutboundCredential(old, field)
		mutation, supplied := next[field]
		if !supplied {
			if len(oldRaw) != 0 {
				next[field] = oldRaw
			}
			continue
		}
		if outbound.Id != 0 && len(oldRaw) != 0 && bytes.Equal(mutation, oldRaw) {
			continue
		}
		var intent struct{ Intent, Value string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil || (!available && intent.Intent != "unchanged") {
			return errOutboundCredentialInvalid
		}
		if intent.Intent == "replace" {
			if err := validateManagedCredential(outbound.Type, field, intent.Value); err != nil {
				return err
			}
		}
		if outbound.Id == 0 {
			if intent.Intent == "clear" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
					return errOutboundCredentialInvalid
				}
				delete(next, field)
				continue
			}
			if intent.Intent != "replace" {
				return errOutboundCredentialInvalid
			}
			placeholder := *outbound
			placeholder.Options = json.RawMessage(`{}`)
			var err error
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Outbound{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			outbound.Id = placeholder.Id
		}
		ref, err := ownerdb.Reference("outbounds", outbound.Id, field)
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		if sealed == "" {
			delete(next, field)
		} else if sealed == captured && len(oldRaw) != 0 {
			next[field] = oldRaw
		} else {
			next[field], _ = json.Marshal(sealed)
		}
	}
	var err error
	outbound.Options, err = json.Marshal(next)
	return err
}

func projectManagedCredentials(outbound model.Outbound, data *map[string]interface{}) error {
	var options map[string]json.RawMessage
	if json.Unmarshal(outbound.Options, &options) != nil {
		return errOutboundCredentialInvalid
	}
	for _, field := range managedCredentialFields(outbound.Type) {
		_, captured := capturedOutboundCredential(options, field)
		ref, err := ownerdb.Reference("outbounds", outbound.Id, field)
		if err != nil {
			return err
		}
		(*data)[field] = ownerdb.View(ref, captured)
	}
	return nil
}

func marshalManagedRuntime(db *gorm.DB, outbound model.Outbound, raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errOutboundCredentialInvalid
	}
	for _, field := range managedCredentialFields(outbound.Type) {
		_, captured := capturedOutboundCredential(fields, field)
		if captured == "" {
			delete(fields, field)
			continue
		}
		if !secretbox.IsEncrypted(captured) {
			return nil, errOutboundCredentialInvalid
		}
		ref, err := ownerdb.Reference("outbounds", outbound.Id, field)
		if err != nil {
			return nil, err
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "outbounds"}, Operation: secretref.UseForOwner, IntentID: "outbound-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			if err := validateManagedCredential(outbound.Type, field, string(value)); err != nil {
				return err
			}
			if field == "cookies" {
				fields[field] = append(json.RawMessage(nil), value...)
			} else {
				fields[field], _ = json.Marshal(string(value))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}
