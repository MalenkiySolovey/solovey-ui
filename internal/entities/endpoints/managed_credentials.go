package endpoints

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

var errEndpointCredential = errors.New("ENDPOINT_CREDENTIAL_INVALID")

// Fixed owner paths preserve the native options tree. A caller cannot choose a
// SecretRef lookup path or supply an envelope belonging to a different row.
func endpointCredentialPaths(kind string) []string {
	if kind != "openconnect" {
		return nil
	}
	return []string{"password", "cookie", "token.secret", "token.pin", "token.password", "tls.client_key", "tls.client_key_password", "tls.mca_key", "tls.mca_key_password", "form_entries"}
}

// Form submissions may contain passwords under arbitrary names. Seal the
// complete ordered bundle rather than guessing which server-defined names
// contain credentials. The SDK still owns the native form schema.
func endpointCredentialPlain(path, value string) (json.RawMessage, error) {
	if len(value) == 0 || len(value) > 1<<20 || strings.ContainsRune(value, 0) {
		return nil, errEndpointCredential
	}
	if path == "form_entries" {
		var entries []json.RawMessage
		if json.Unmarshal([]byte(value), &entries) != nil || entries == nil {
			return nil, errEndpointCredential
		}
		for _, entry := range entries {
			if jsonvalue.StrictObject(entry) != nil {
				return nil, errEndpointCredential
			}
		}
		return json.RawMessage(value), nil
	}
	return json.Marshal(value)
}

func endpointCredentialField(root map[string]json.RawMessage, path string, write *json.RawMessage) (json.RawMessage, error) {
	key, rest, nested := strings.Cut(path, ".")
	for candidate := range root {
		if candidate != key && strings.EqualFold(candidate, key) {
			return nil, errEndpointCredential
		}
	}
	if !nested {
		previous := root[key]
		if write != nil {
			if len(*write) == 0 {
				delete(root, key)
			} else {
				root[key] = *write
			}
		}
		return previous, nil
	}
	child := map[string]json.RawMessage{}
	if raw := root[key]; len(raw) > 0 && (jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &child) != nil || child == nil) {
		return nil, errEndpointCredential
	}
	previous, err := endpointCredentialField(child, rest, write)
	if err == nil && write != nil {
		if len(child) == 0 {
			delete(root, key)
		} else {
			root[key], err = json.Marshal(child)
		}
	}
	return previous, err
}

func endpointCredentialObject(raw []byte) (map[string]json.RawMessage, error) {
	values := map[string]json.RawMessage{}
	if len(raw) > 0 && (jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &values) != nil || values == nil) {
		return nil, errEndpointCredential
	}
	return values, nil
}

func prepareEndpointCredentials(tx *gorm.DB, endpoint *model.Endpoint) error {
	paths := endpointCredentialPaths(endpoint.Type)
	if len(paths) == 0 {
		return nil
	}
	next, err := endpointCredentialObject(endpoint.Options)
	if err != nil {
		return err
	}
	old := map[string]json.RawMessage{}
	if endpoint.Id != 0 {
		var stored model.Endpoint
		if err := tx.Select("id", "type", "options").First(&stored, endpoint.Id).Error; err != nil {
			return err
		}
		if stored.Type == endpoint.Type {
			old, err = endpointCredentialObject(stored.Options)
			if err != nil {
				return err
			}
		}
	}
	for _, path := range paths {
		previous, err := endpointCredentialField(old, path, nil)
		if err != nil {
			return err
		}
		mutation, err := endpointCredentialField(next, path, nil)
		if err != nil {
			return err
		}
		if len(mutation) == 0 || len(previous) > 0 && bytes.Equal(mutation, previous) {
			if len(previous) > 0 {
				if _, err := endpointCredentialField(next, path, &previous); err != nil {
					return err
				}
			}
			continue
		}
		var intent struct{ Intent, Value string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil {
			return errEndpointCredential
		}
		if intent.Intent == "replace" {
			if _, err := endpointCredentialPlain(path, intent.Value); err != nil {
				return err
			}
		}
		if endpoint.Id == 0 {
			if intent.Intent == "clear" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
					return errEndpointCredential
				}
				var empty json.RawMessage
				if _, err := endpointCredentialField(next, path, &empty); err != nil {
					return err
				}
				continue
			}
			if intent.Intent != "replace" {
				return errEndpointCredential
			}
			placeholder := *endpoint
			placeholder.Options = json.RawMessage(`{}`)
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Endpoint{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			endpoint.Id = placeholder.Id
		}
		var captured string
		if len(previous) > 0 && json.Unmarshal(previous, &captured) != nil {
			return errEndpointCredential
		}
		ref, err := ownerdb.Reference("endpoints", endpoint.Id, path)
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		var raw json.RawMessage
		if sealed != "" {
			raw, _ = json.Marshal(sealed)
		}
		if _, err := endpointCredentialField(next, path, &raw); err != nil {
			return err
		}
	}
	endpoint.Options, err = json.Marshal(next)
	return err
}

func projectEndpointCredentials(endpoint model.Endpoint, data map[string]interface{}) error {
	if len(endpointCredentialPaths(endpoint.Type)) == 0 {
		return nil
	}
	values, err := endpointCredentialObject(endpoint.Options)
	if err != nil {
		return err
	}
	for _, path := range endpointCredentialPaths(endpoint.Type) {
		raw, err := endpointCredentialField(values, path, nil)
		if err != nil {
			return err
		}
		var captured string
		_ = json.Unmarshal(raw, &captured)
		ref, err := ownerdb.Reference("endpoints", endpoint.Id, path)
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(ownerdb.View(ref, captured))
		view := json.RawMessage(encoded)
		if _, err := endpointCredentialField(values, path, &view); err != nil {
			return err
		}
	}
	for key, value := range values {
		switch key {
		case "id", "sortOrder", "type", "tag", "ext":
			continue
		}
		data[key] = value
	}
	return nil
}

func marshalEndpointRuntime(db *gorm.DB, endpoint model.Endpoint) ([]byte, error) {
	raw, err := endpoint.MarshalJSON()
	if err != nil || len(endpointCredentialPaths(endpoint.Type)) == 0 {
		return raw, err
	}
	values, err := endpointCredentialObject(raw)
	if err != nil {
		return nil, err
	}
	for _, path := range endpointCredentialPaths(endpoint.Type) {
		raw, err := endpointCredentialField(values, path, nil)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		var captured string
		if json.Unmarshal(raw, &captured) != nil || !secretbox.IsEncrypted(captured) {
			return nil, errEndpointCredential
		}
		ref, err := ownerdb.Reference("endpoints", endpoint.Id, path)
		if err != nil {
			return nil, err
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "endpoints"}, Operation: secretref.UseForOwner, IntentID: "endpoint-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			plain, err := endpointCredentialPlain(path, string(value))
			if err != nil {
				return err
			}
			_, err = endpointCredentialField(values, path, &plain)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(values)
}

// The import adapter accepts native SDK values, never a caller-selected secret
// reference. Captured envelopes can only be rebound from this exact old row.
// Recreate decrypts before removing that row and seals against the new ID.
func importEndpointCredentials(tx *gorm.DB, endpoint *model.Endpoint, recreate bool) error {
	if len(endpointCredentialPaths(endpoint.Type)) == 0 {
		return nil
	}
	values, err := endpointCredentialObject(endpoint.Options)
	if err != nil {
		return err
	}
	var previous model.Endpoint
	if endpoint.Id != 0 {
		if err := tx.First(&previous, endpoint.Id).Error; err != nil {
			return err
		}
	}
	for _, path := range endpointCredentialPaths(endpoint.Type) {
		raw, err := endpointCredentialField(values, path, nil)
		if err != nil {
			return err
		}
		if len(raw) == 0 {
			continue
		}
		var value string
		if path == "form_entries" && len(raw) > 0 && raw[0] == '[' {
			value = string(raw)
		} else if json.Unmarshal(raw, &value) != nil {
			if path != "tls.client_key" && path != "tls.mca_key" {
				return errEndpointCredential
			}
			var lines []string
			if json.Unmarshal(raw, &lines) != nil {
				return errEndpointCredential
			}
			value = strings.Join(lines, "\n")
		}
		if secretbox.IsEncrypted(value) {
			old, err := endpointCredentialObject(previous.Options)
			if err != nil || previous.Id == 0 || previous.Type != endpoint.Type {
				return errEndpointCredential
			}
			captured, err := endpointCredentialField(old, path, nil)
			if err != nil || !bytes.Equal(captured, raw) {
				return errEndpointCredential
			}
			if !recreate {
				continue
			}
			ref, err := ownerdb.Reference("endpoints", previous.Id, path)
			if err != nil {
				return err
			}
			request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "endpoints"}, Operation: secretref.UseForOwner, IntentID: "endpoint-import"}
			_, err = ownerdb.Access(tx.Statement.Context, tx, request, value, func(plain []byte) error { value = string(plain); return nil })
			if err != nil {
				return err
			}
		}
		intent := map[string]string{"intent": "replace", "value": value}
		if value == "" {
			intent = map[string]string{"intent": "clear"}
		}
		encoded, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		mutation := json.RawMessage(encoded)
		if _, err := endpointCredentialField(values, path, &mutation); err != nil {
			return err
		}
	}
	endpoint.Options, err = json.Marshal(values)
	return err
}
