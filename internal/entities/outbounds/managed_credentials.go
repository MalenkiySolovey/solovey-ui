package outbounds

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

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
	case "masque":
		return []string{"auth_token", "private_key", "license_key"}
	case "sudoku":
		return []string{"key"}
	case "hysteria2":
		return []string{"token"}
	default:
		return nil
	}
}

func managedCredentialContainer(kind string) string {
	switch kind {
	case "masque":
		return "profile"
	case "hysteria2":
		return "realm"
	default:
		return ""
	}
}

// Nested native objects keep their declared shape and use the same row-bound
// lifecycle as top-level credentials. Metadata never replaces a secret.
func managedCredentialOptions(kind string, root map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	values := root
	if container := managedCredentialContainer(kind); container != "" {
		for key := range root {
			if key != container && strings.EqualFold(key, container) {
				return nil, errOutboundCredentialInvalid
			}
		}
		values = map[string]json.RawMessage{}
		if raw, present := root[container]; present {
			if kind == "hysteria2" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return nil, nil
			}
			if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &values) != nil || values == nil {
				return nil, errOutboundCredentialInvalid
			}
		} else if kind == "hysteria2" {
			return nil, nil // An absent optional Realm must stay absent.
		}
	}
	for key := range values {
		for _, field := range managedCredentialFields(kind) {
			if key != field && strings.EqualFold(key, field) {
				return nil, errOutboundCredentialInvalid
			}
		}
	}
	return values, nil
}

func storeManagedCredentialOptions(kind string, root, values map[string]json.RawMessage) error {
	if container := managedCredentialContainer(kind); container != "" && values != nil {
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		root[container] = raw
	}
	return nil
}

func managedCredentialReference(kind string, id uint, field string) (secretref.Reference, error) {
	if container := managedCredentialContainer(kind); container != "" {
		field = container + "." + field
	}
	return ownerdb.Reference("outbounds", id, field)
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
	if kind == "hysteria2" && field == "token" {
		if calloptions.CredentialValid("password", value) {
			return nil
		}
		return errOutboundCredentialInvalid
	}
	if kind == "masque" {
		if (field == "auth_token" || field == "private_key" || field == "license_key") && calloptions.CredentialValid("password", value) {
			return nil
		}
		return errOutboundCredentialInvalid
	}
	if kind == "sudoku" && field == "key" {
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

// PrepareImportedCredentialDraft adapts captured native plaintext to explicit
// owner intents. Only trusted import paths call it; ordinary saves still reject
// raw credentials and references/envelopes supplied by another source.
func PrepareImportedCredentialDraft(raw json.RawMessage) (json.RawMessage, error) {
	var header struct{ Type string }
	if json.Unmarshal(raw, &header) != nil {
		return nil, errOutboundCredentialInvalid
	}
	if len(managedCredentialFields(header.Type)) == 0 {
		return raw, nil
	}
	var root map[string]json.RawMessage
	if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &root) != nil {
		return nil, errOutboundCredentialInvalid
	}
	values, err := managedCredentialOptions(header.Type, root)
	if err != nil || values == nil {
		return raw, err
	}
	for _, field := range managedCredentialFields(header.Type) {
		captured, present := values[field]
		if !present {
			continue
		}
		var value string
		if field == "cookies" {
			var cookies []json.RawMessage
			if json.Unmarshal(captured, &cookies) != nil {
				return nil, errOutboundCredentialInvalid
			}
			if len(cookies) != 0 {
				value = string(captured)
			}
		} else if json.Unmarshal(captured, &value) != nil {
			return nil, errOutboundCredentialInvalid
		}
		if secretbox.IsEncrypted(value) {
			return nil, errOutboundCredentialInvalid
		}
		if value == "" {
			values[field] = json.RawMessage(`{"intent":"clear"}`)
			continue
		}
		if err := validateManagedCredential(header.Type, field, value); err != nil {
			return nil, err
		}
		values[field], err = json.Marshal(map[string]string{"intent": "replace", "value": value})
		if err != nil {
			return nil, err
		}
	}
	if err := storeManagedCredentialOptions(header.Type, root, values); err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

func prepareManagedCredentials(tx *gorm.DB, outbound *model.Outbound) error {
	var nextRoot, oldRoot map[string]json.RawMessage
	if jsonvalue.StrictObject(outbound.Options) != nil || json.Unmarshal(outbound.Options, &nextRoot) != nil || nextRoot == nil {
		return errOutboundCredentialInvalid
	}
	if outbound.Id != 0 {
		var stored model.Outbound
		if err := tx.Select("id", "type", "options").First(&stored, outbound.Id).Error; err != nil {
			return err
		}
		if stored.Type == outbound.Type && len(stored.Options) != 0 && json.Unmarshal(stored.Options, &oldRoot) != nil {
			return errOutboundCredentialInvalid
		}
	}
	next, err := managedCredentialOptions(outbound.Type, nextRoot)
	if err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	old, err := managedCredentialOptions(outbound.Type, oldRoot)
	if err != nil {
		return err
	}
	available := entitycapabilities.ResolveResource("outbounds", outbound.Type, outbound.Options).Available
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
		ref, err := managedCredentialReference(outbound.Type, outbound.Id, field)
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
	if err := storeManagedCredentialOptions(outbound.Type, nextRoot, next); err != nil {
		return err
	}
	outbound.Options, err = json.Marshal(nextRoot)
	return err
}

func projectManagedCredentials(outbound model.Outbound, data *map[string]interface{}) error {
	var options map[string]json.RawMessage
	if json.Unmarshal(outbound.Options, &options) != nil {
		return errOutboundCredentialInvalid
	}
	values, err := managedCredentialOptions(outbound.Type, options)
	if err != nil {
		return err
	}
	if values == nil {
		return nil
	}
	for _, field := range managedCredentialFields(outbound.Type) {
		_, captured := capturedOutboundCredential(values, field)
		ref, err := managedCredentialReference(outbound.Type, outbound.Id, field)
		if err != nil {
			return err
		}
		view := ownerdb.View(ref, captured)
		if managedCredentialContainer(outbound.Type) != "" {
			values[field], _ = json.Marshal(view)
		} else {
			(*data)[field] = view
		}
	}
	if container := managedCredentialContainer(outbound.Type); container != "" {
		(*data)[container] = values
	}
	return nil
}

func marshalManagedRuntime(db *gorm.DB, outbound model.Outbound, raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errOutboundCredentialInvalid
	}
	values, err := managedCredentialOptions(outbound.Type, fields)
	if err != nil {
		return nil, err
	}
	if values == nil {
		return raw, nil
	}
	for _, field := range managedCredentialFields(outbound.Type) {
		_, captured := capturedOutboundCredential(values, field)
		if captured == "" {
			delete(values, field)
			continue
		}
		if !secretbox.IsEncrypted(captured) {
			return nil, errOutboundCredentialInvalid
		}
		ref, err := managedCredentialReference(outbound.Type, outbound.Id, field)
		if err != nil {
			return nil, err
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "outbounds"}, Operation: secretref.UseForOwner, IntentID: "outbound-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			if err := validateManagedCredential(outbound.Type, field, string(value)); err != nil {
				return err
			}
			if field == "cookies" {
				values[field] = append(json.RawMessage(nil), value...)
			} else {
				values[field], _ = json.Marshal(string(value))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if err := storeManagedCredentialOptions(outbound.Type, fields, values); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
