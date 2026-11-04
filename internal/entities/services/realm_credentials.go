package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

var errRealmCredential = errors.New("HYSTERIA_REALM_CREDENTIAL_INVALID")

// Realm identities authorize the rendezvous service and its realm quota. They
// are not traffic-accounting Users. Their tokens belong to this Services row;
// renaming an identity requires a new token rather than moving another name's
// credential. Hashing the validated name gives a stable, bounded owner field.
func realmTokenReference(id uint, name string) (secretref.Reference, error) {
	digest := sha256.Sum256([]byte(name))
	return ownerdb.Reference("services", id, "realm.users."+hex.EncodeToString(digest[:])+".token")
}

type realmCredentialOptions struct {
	root  map[string]json.RawMessage
	users []map[string]json.RawMessage
}

func realmOptions(raw []byte) (*realmCredentialOptions, error) {
	var root map[string]json.RawMessage
	if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, errRealmCredential
	}
	for key := range root {
		if key != "users" && strings.EqualFold(key, "users") {
			return nil, errRealmCredential
		}
	}
	var entries []json.RawMessage
	if json.Unmarshal(root["users"], &entries) != nil || len(entries) == 0 {
		return nil, errRealmCredential
	}
	result := &realmCredentialOptions{root: root}
	seen := map[string]bool{}
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if jsonvalue.StrictObject(entry) != nil || json.Unmarshal(entry, &fields) != nil {
			return nil, errRealmCredential
		}
		for key := range fields {
			for _, fixed := range []string{"name", "token", "max_realms"} {
				if key != fixed && strings.EqualFold(key, fixed) {
					return nil, errRealmCredential
				}
			}
		}
		name := realmName(fields)
		var limit int
		if strings.TrimSpace(name) == "" || strings.ContainsRune(name, 0) || seen[name] ||
			len(fields["max_realms"]) > 0 && (json.Unmarshal(fields["max_realms"], &limit) != nil || limit < 0) {
			return nil, errRealmCredential
		}
		seen[name] = true
		result.users = append(result.users, fields)
	}
	return result, nil
}

func realmName(fields map[string]json.RawMessage) string {
	var name string
	_ = json.Unmarshal(fields["name"], &name)
	return name
}

func (o *realmCredentialOptions) marshal() ([]byte, error) {
	var err error
	o.root["users"], err = json.Marshal(o.users)
	if err != nil {
		return nil, err
	}
	return json.Marshal(o.root)
}

func prepareRealmCredentials(tx *gorm.DB, row *model.Service) error {
	if row.Type != "hysteria-realm" {
		return nil
	}
	next, err := realmOptions(row.Options)
	if err != nil {
		return err
	}
	previous := map[string]json.RawMessage{}
	if row.Id != 0 {
		var old model.Service
		if err := tx.First(&old, row.Id).Error; err != nil {
			return err
		}
		if old.Type == row.Type {
			captured, err := realmOptions(old.Options)
			if err != nil {
				return err
			}
			for _, user := range captured.users {
				previous[realmName(user)] = user["token"]
			}
		}
	}
	for _, user := range next.users {
		name := realmName(user)
		old, mutation := previous[name], user["token"]
		if len(mutation) == 0 || len(old) > 0 && bytes.Equal(old, mutation) {
			if len(old) == 0 {
				return errRealmCredential
			}
			user["token"] = old
			continue
		}
		var intent struct{ Intent, Value string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil {
			return errRealmCredential
		}
		// A realm identity must always authenticate. Remove the identity to
		// revoke it; Clear must never create an anonymous rendezvous service.
		if intent.Intent == "clear" {
			return errRealmCredential
		}
		if intent.Intent == "replace" && !realmTokenValid(intent.Value) {
			return errRealmCredential
		}
		if row.Id == 0 {
			if intent.Intent != "replace" {
				return errRealmCredential
			}
			placeholder := *row
			placeholder.Options = json.RawMessage(`{}`)
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Service{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			row.Id = placeholder.Id
		}
		var captured string
		if len(old) > 0 && json.Unmarshal(old, &captured) != nil {
			return errRealmCredential
		}
		ref, err := realmTokenReference(row.Id, name)
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		if !secretbox.IsEncrypted(sealed) {
			return errRealmCredential
		}
		user["token"], _ = json.Marshal(sealed)
	}
	row.Options, err = next.marshal()
	return err
}

func realmTokenValid(value string) bool {
	return len(value) > 0 && len(value) <= 64<<10 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func projectRealmCredentials(row model.Service, data map[string]interface{}) error {
	if row.Type != "hysteria-realm" {
		return nil
	}
	options, err := realmOptions(row.Options)
	if err != nil {
		return err
	}
	for _, user := range options.users {
		var captured string
		if json.Unmarshal(user["token"], &captured) != nil || !secretbox.IsEncrypted(captured) {
			return errRealmCredential
		}
		ref, err := realmTokenReference(row.Id, realmName(user))
		if err != nil {
			return err
		}
		user["token"], _ = json.Marshal(ownerdb.View(ref, captured))
	}
	data["users"] = options.users
	return nil
}

func marshalServiceRuntime(db *gorm.DB, row model.Service) ([]byte, error) {
	raw, err := row.MarshalJSON()
	if err != nil || row.Type != "hysteria-realm" {
		return raw, err
	}
	options, err := realmOptions(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, user := range options.users {
		var captured string
		if json.Unmarshal(user["token"], &captured) != nil || !secretbox.IsEncrypted(captured) {
			return nil, errRealmCredential
		}
		ref, err := realmTokenReference(row.Id, realmName(user))
		if err != nil {
			return nil, err
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "services"}, Operation: secretref.UseForOwner, IntentID: "service-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			if !realmTokenValid(string(value)) || seen[string(value)] {
				return errRealmCredential
			}
			seen[string(value)] = true
			user["token"], _ = json.Marshal(string(value))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return options.marshal()
}
