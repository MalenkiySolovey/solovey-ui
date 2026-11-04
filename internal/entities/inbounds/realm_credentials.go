package entityinbounds

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/calloptions"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

// Realm authenticates the serving rendezvous connection. It belongs to this
// Inbound row; Hysteria data-plane users remain with the existing Users owner.
func inboundRealmFields(raw json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	var root map[string]json.RawMessage
	if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &root) != nil {
		return nil, nil, errInboundCredentialInvalid
	}
	for key := range root {
		if key != "realm" && strings.EqualFold(key, "realm") {
			return nil, nil, errInboundCredentialInvalid
		}
	}
	rawRealm := bytes.TrimSpace(root["realm"])
	if len(rawRealm) == 0 || bytes.Equal(rawRealm, []byte("null")) {
		return root, nil, nil
	}
	var realm map[string]json.RawMessage
	if jsonvalue.StrictObject(rawRealm) != nil || json.Unmarshal(rawRealm, &realm) != nil {
		return nil, nil, errInboundCredentialInvalid
	}
	for key := range realm {
		if key != "token" && strings.EqualFold(key, "token") {
			return nil, nil, errInboundCredentialInvalid
		}
	}
	return root, realm, nil
}

func prepareRealmCredentials(tx *gorm.DB, inbound *model.Inbound) error {
	root, realm, err := inboundRealmFields(inbound.Options)
	if err != nil || realm == nil {
		return err
	}
	var captured string
	var oldRaw json.RawMessage
	if inbound.Id != 0 {
		var stored model.Inbound
		if err := tx.Select("id", "type", "options").First(&stored, inbound.Id).Error; err != nil {
			return err
		}
		if stored.Type == inbound.Type {
			_, oldRealm, err := inboundRealmFields(stored.Options)
			if err != nil {
				return err
			}
			oldRaw = oldRealm["token"]
			if len(oldRaw) != 0 && json.Unmarshal(oldRaw, &captured) != nil {
				return errInboundCredentialInvalid
			}
		}
	}
	mutation, supplied := realm["token"]
	if !supplied {
		if len(oldRaw) != 0 {
			realm["token"] = oldRaw
		}
	} else if len(oldRaw) == 0 || !bytes.Equal(mutation, oldRaw) {
		var intent struct{ Intent, Value string }
		if json.Unmarshal(mutation, &intent) != nil || !entitycapabilities.ResolveResource("inbounds", inbound.Type, inbound.Options).Available {
			return errInboundCredentialInvalid
		}
		if intent.Intent == "replace" && !calloptions.CredentialValid("password", intent.Value) {
			return errInboundCredentialInvalid
		}
		if inbound.Id == 0 {
			if intent.Intent != "replace" && intent.Intent != "clear" {
				return errInboundCredentialInvalid
			}
			placeholder := *inbound
			placeholder.Options, placeholder.OutJson, placeholder.Tls = json.RawMessage(`{}`), json.RawMessage(`null`), nil
			placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Inbound{}, 0)
			if err != nil {
				return err
			}
			if err := tx.Create(&placeholder).Error; err != nil {
				return err
			}
			inbound.Id = placeholder.Id
		}
		ref, err := ownerdb.Reference("inbounds", inbound.Id, "realm.token")
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		if sealed == "" {
			delete(realm, "token")
		} else {
			realm["token"], _ = json.Marshal(sealed)
		}
	}
	root["realm"], _ = json.Marshal(realm)
	inbound.Options, err = json.Marshal(root)
	return err
}

func renderRealmCredentials(db *gorm.DB, inbound model.Inbound, raw []byte, masked bool) ([]byte, error) {
	root, realm, err := inboundRealmFields(raw)
	if err != nil || realm == nil {
		return raw, err
	}
	var captured string
	if token := realm["token"]; len(token) != 0 && json.Unmarshal(token, &captured) != nil {
		return nil, errInboundCredentialInvalid
	}
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "realm.token")
	if err != nil {
		return nil, err
	}
	if masked {
		realm["token"], err = json.Marshal(ownerdb.View(ref, captured))
	} else if captured != "" {
		if !secretbox.IsEncrypted(captured) {
			return nil, errInboundCredentialInvalid
		}
		request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "inbounds"}, Operation: secretref.UseForOwner, IntentID: "hysteria-realm-config"}
		_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
			realm["token"], _ = json.Marshal(string(value))
			return nil
		})
	}
	if err != nil {
		return nil, err
	}
	root["realm"], _ = json.Marshal(realm)
	return json.Marshal(root)
}

// Called only by the existing private Users/subscription delivery path. The
// public template omits the serving token; the same owner borrows it here just
// as transport credentials are supplied for an assigned user's client config.
func realmOutboundDeliveryCopy(db *gorm.DB, inbound *model.Inbound, realm map[string]json.RawMessage) (*model.Inbound, error) {
	var template map[string]json.RawMessage
	if json.Unmarshal(inbound.OutJson, &template) != nil {
		return nil, errInboundCredentialInvalid
	}
	var public map[string]json.RawMessage
	if json.Unmarshal(template["realm"], &public) != nil || public == nil {
		return nil, errInboundCredentialInvalid
	}
	// Keep host DNS references and HTTP client policy out of the portable
	// endpoint; clients retain their own resolver and HTTP transport authority.
	if token := realm["token"]; len(token) != 0 {
		public["token"] = token
	}
	template["realm"], _ = json.Marshal(public)
	raw, _ := json.Marshal(template)
	raw, err := renderRealmCredentials(db, *inbound, raw, false)
	if err != nil {
		return nil, err
	}
	inbound.OutJson = raw
	return inbound, nil
}
