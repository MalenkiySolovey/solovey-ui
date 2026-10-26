// Package credentials adapts Users-owned protocol credential fields to the
// common SecretRef contract. Client.config remains the existing owner backend;
// this package creates no tables, API, key lifecycle or runtime authority.
package credentials

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

var ErrInvalid = errors.New("CLIENT_CREDENTIAL_INVALID")

// ValidateClientConfig checks managed input before any older owner helper can
// normalize its JSON. Ambiguous duplicate fields cannot acquire save authority.
func ValidateClientConfig(config json.RawMessage) error {
	if len(config) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) != nil {
		return ErrInvalid
	}
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField != "" && len(root[fact.UserField]) > 0 && jsonvalue.StrictObject(config) != nil {
			return ErrInvalid
		}
	}
	return nil
}

func clientField(clientID uint, protocol string) (string, secretref.Reference, error) {
	fact, ok := clientfacts.Resolve(protocol)
	if !ok || fact.SecretField == "" || fact.UserField == "" {
		return "", secretref.Reference{}, secretref.ErrDenied
	}
	ref, err := ownerdb.Reference("users", clientID, fact.UserField+"."+fact.SecretField)
	return fact.SecretField, ref, err
}

// ProjectClient exposes presence and a field-bound reference, never ciphertext
// or plaintext. Ordinary list/read APIs cannot use this as a reveal operation.
func ProjectClient(clientID uint, config json.RawMessage) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if len(config) == 0 {
		return config, nil
	}
	if json.Unmarshal(config, &root) != nil {
		return nil, ErrInvalid
	}
	changed := false
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField == "" || len(root[fact.UserField]) == 0 {
			continue
		}
		if jsonvalue.StrictObject(config) != nil {
			return nil, ErrInvalid
		}
		var credential map[string]json.RawMessage
		if json.Unmarshal(root[fact.UserField], &credential) != nil || credential == nil {
			return nil, ErrInvalid
		}
		raw := credential[fact.SecretField]
		var captured string
		if len(raw) > 0 && json.Unmarshal(raw, &captured) != nil {
			if !clientfacts.DeliveryOnly(fact.Type) {
				return nil, ErrInvalid
			}
			// Historical split maps are private too. Mask them without granting
			// runtime use or silently rewriting the stored representation.
			captured = string(raw)
		}
		_, ref, err := clientField(clientID, fact.Type)
		if err != nil {
			return nil, err
		}
		credential[fact.SecretField], _ = json.Marshal(ownerdb.View(ref, captured))
		root[fact.UserField], _ = json.Marshal(credential)
		changed = true
	}
	if !changed {
		return config, nil
	}
	return json.Marshal(root)
}

// ApplyClientField validates the explicit owner mutation on a resolved client.
// Capability admission and commit remain with the existing Client save owner.
func ApplyClientField(db *gorm.DB, clientID uint, protocol string, current string, mutation json.RawMessage) (string, error) {
	_, ref, err := clientField(clientID, protocol)
	if err != nil {
		return "", err
	}
	if err := validateReplacement(protocol, mutation); err != nil {
		return "", err
	}
	return ownerdb.Apply(db, ref, current, mutation)
}

// UseClient borrows a protocol credential only within the supplied Users owner
// operation. The reference comes from trusted client identity and schema facts.
// Consumers of historical unmigrated protocols keep their accepted backend.
func UseClient(ctx context.Context, db *gorm.DB, clientID uint, protocol string, credential json.RawMessage, intentID string, use func([]byte) error) error {
	fact, ok := clientfacts.Resolve(protocol)
	if !ok || use == nil {
		return secretref.ErrDenied
	}
	if fact.SecretField == "" {
		return use(credential)
	}
	if jsonvalue.StrictObject(credential) != nil {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(credential, &fields) != nil {
		return ErrInvalid
	}
	var captured string
	if raw := fields[fact.SecretField]; len(raw) > 0 && json.Unmarshal(raw, &captured) != nil {
		return ErrInvalid
	}
	if captured == "" || !secretbox.IsEncrypted(captured) {
		return secretref.ErrUnavailable
	}
	_, ref, err := clientField(clientID, protocol)
	if err != nil {
		return err
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "users"}, Operation: secretref.UseForOwner, IntentID: intentID}
	_, err = ownerdb.Access(ctx, db, request, captured, func(value []byte) error {
		if clientfacts.DeliveryOnly(protocol) {
			bag, err := SudokuKeyBag(string(value))
			if err != nil {
				return err
			}
			fields[fact.SecretField], _ = json.Marshal(bag)
		} else {
			fields[fact.SecretField], _ = json.Marshal(string(value))
		}
		raw, err := json.Marshal(fields)
		defer common.WipeBytes(raw)
		if err != nil {
			return ErrInvalid
		}
		return use(raw)
	})
	return err
}
