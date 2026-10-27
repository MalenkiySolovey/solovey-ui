package entityinbounds

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

// Cookie bundles and account passwords use the existing owner row and common
// SecretRef lifecycle. Call introduces no credential store or client owner.
var callCredentialFields = [...]string{"cookies", "password", "join_link"}

func capturedCallCredential(options map[string]json.RawMessage, field string) (json.RawMessage, string) {
	raw := options[field]
	var captured string
	if len(raw) != 0 && json.Unmarshal(raw, &captured) != nil {
		captured = string(raw)
	}
	return raw, captured
}

func validateCallCredential(field, value string) error {
	if len(value) == 0 || len(value) > 64<<10 || strings.ContainsRune(value, 0) {
		return errInboundCredentialInvalid
	}
	if field == "password" {
		return nil
	}
	if field == "join_link" {
		if len(value) > 4096 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
			return errInboundCredentialInvalid
		}
		return nil
	}
	var cookies []json.RawMessage
	if json.Unmarshal([]byte(value), &cookies) != nil || len(cookies) == 0 || len(cookies) > 128 {
		return errInboundCredentialInvalid
	}
	seen := make(map[string]bool, len(cookies))
	for _, raw := range cookies {
		var cookie struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if jsonvalue.StrictObject(raw) != nil || decoder.Decode(&cookie) != nil || seen[cookie.Name] || (&http.Cookie{Name: cookie.Name, Value: cookie.Value}).Valid() != nil {
			return errInboundCredentialInvalid
		}
		seen[cookie.Name] = true
	}
	return nil
}

func prepareCallCredentials(tx *gorm.DB, inbound *model.Inbound) error {
	var next, old map[string]json.RawMessage
	if jsonvalue.StrictObject(inbound.Options) != nil || json.Unmarshal(inbound.Options, &next) != nil || next == nil {
		return errInboundCredentialInvalid
	}
	if inbound.Id != 0 {
		var stored model.Inbound
		if err := tx.Select("id", "type", "options").First(&stored, inbound.Id).Error; err != nil {
			return err
		}
		if stored.Type == inbound.Type && len(stored.Options) != 0 && json.Unmarshal(stored.Options, &old) != nil {
			return errInboundCredentialInvalid
		}
	}
	available := entitycapabilities.Resolve("inbounds", "call").Available
	for _, field := range callCredentialFields {
		oldRaw, captured := capturedCallCredential(old, field)
		mutation, supplied := next[field]
		if !supplied {
			if len(oldRaw) != 0 {
				next[field] = oldRaw
			}
			continue
		}
		if inbound.Id != 0 && len(oldRaw) != 0 && bytes.Equal(mutation, oldRaw) {
			continue
		}
		var intent struct{ Intent, Value string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil || (!available && intent.Intent != "unchanged") {
			return errInboundCredentialInvalid
		}
		if intent.Intent == "replace" {
			if err := validateCallCredential(field, intent.Value); err != nil {
				return err
			}
		}
		if inbound.Id == 0 {
			if intent.Intent == "clear" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
					return errInboundCredentialInvalid
				}
				delete(next, field)
				continue
			}
			if intent.Intent != "replace" {
				return errInboundCredentialInvalid
			}
			placeholder := *inbound
			placeholder.Options, placeholder.OutJson, placeholder.Tls = json.RawMessage(`{}`), json.RawMessage(`null`), nil
			var err error
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Inbound{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			inbound.Id = placeholder.Id
		}
		ref, err := ownerdb.Reference("inbounds", inbound.Id, field)
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
	inbound.Options, err = json.Marshal(next)
	return err
}

func projectCallCredentials(inbound model.Inbound, data *map[string]interface{}) error {
	var options map[string]json.RawMessage
	if json.Unmarshal(inbound.Options, &options) != nil {
		return errInboundCredentialInvalid
	}
	for _, field := range callCredentialFields {
		_, captured := capturedCallCredential(options, field)
		ref, err := ownerdb.Reference("inbounds", inbound.Id, field)
		if err != nil {
			return err
		}
		(*data)[field] = ownerdb.View(ref, captured)
	}
	return nil
}

func marshalCallRuntime(db *gorm.DB, inbound model.Inbound, raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errInboundCredentialInvalid
	}
	for _, field := range callCredentialFields {
		_, captured := capturedCallCredential(fields, field)
		if captured == "" {
			delete(fields, field)
			continue
		}
		if !secretbox.IsEncrypted(captured) {
			return nil, errInboundCredentialInvalid
		}
		ref, err := ownerdb.Reference("inbounds", inbound.Id, field)
		if err != nil {
			return nil, err
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "inbounds"}, Operation: secretref.UseForOwner, IntentID: "inbound-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			if err := validateCallCredential(field, string(value)); err != nil {
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
