package entityclients

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

// prepareManagedCredentials is inside the existing Client save transaction.
// New encrypted fields require an available serving protocol. Historical
// unavailable credentials may be retained exactly; changing them is blocked.
func prepareManagedCredentials(tx *gorm.DB, client *model.Client, editing bool) error {
	var proposed map[string]json.RawMessage
	if len(client.Config) > 0 && json.Unmarshal(client.Config, &proposed) != nil {
		return credentials.ErrInvalid
	}
	if proposed == nil {
		proposed = map[string]json.RawMessage{}
	}
	stored := map[string]json.RawMessage{}
	var old model.Client
	if editing {
		if err := tx.Select("config", "inbounds").Where("id = ?", client.Id).First(&old).Error; err != nil {
			return err
		}
		if len(old.Config) != 0 && json.Unmarshal(old.Config, &stored) != nil {
			return credentials.ErrInvalid
		}
	}
	var ids, oldIDs []uint
	if len(client.Inbounds) > 0 && json.Unmarshal(client.Inbounds, &ids) != nil || len(old.Inbounds) > 0 && json.Unmarshal(old.Inbounds, &oldIDs) != nil {
		return credentials.ErrInvalid
	}
	oldMembership := map[uint]bool{}
	for _, id := range oldIDs {
		oldMembership[id] = true
	}
	var inbounds []model.Inbound
	if len(ids) > 0 {
		if err := tx.Select("id", "type").Where("id IN ?", ids).Find(&inbounds).Error; err != nil {
			return err
		}
	}
	assigned := map[string]bool{}
	for _, inbound := range inbounds {
		fact, known := clientfacts.Resolve(inbound.Type)
		if !known || fact.SecretField == "" {
			continue
		}
		available := entitycapabilities.Resolve("inbounds", fact.Type).Available
		if !available && !oldMembership[inbound.Id] {
			return common.NewError("CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE")
		}
		assigned[fact.Type] = available
	}
	managed := []clientfacts.Fact{}
	for _, fact := range clientfacts.Facts() {
		if fact.SecretField == "" {
			continue
		}
		if len(proposed[fact.UserField]) == 0 && len(stored[fact.UserField]) > 0 {
			proposed[fact.UserField] = stored[fact.UserField]
		}
		if len(proposed[fact.UserField]) == 0 && assigned[fact.Type] {
			// Assignment is an existing Users owner mutation. Initialize only
			// a missing credential, once, on an admitted serving protocol.
			value, err := credentials.Generate(fact.Type)
			if err != nil {
				return credentials.ErrInvalid
			}
			proposed[fact.UserField], _ = json.Marshal(map[string]any{"name": client.Name,
				fact.SecretField: map[string]string{"intent": "replace", "value": value}})
		}
		if len(proposed[fact.UserField]) != 0 {
			managed = append(managed, fact)
		}
	}
	if len(managed) == 0 {
		return nil
	}
	if len(client.Config) > 0 && jsonvalue.StrictObject(client.Config) != nil || len(old.Config) > 0 && jsonvalue.StrictObject(old.Config) != nil {
		return credentials.ErrInvalid
	}
	// Preflight every field before reserving identity, creating a key, or
	// recording a mutation. Unknown critical capabilities are unavailable.
	for _, fact := range managed {
		if entitycapabilities.Resolve("inbounds", fact.Type).Available {
			continue
		}
		if !editing || len(stored[fact.UserField]) == 0 {
			return common.NewError("CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE")
		}
		var next, old map[string]json.RawMessage
		if json.Unmarshal(proposed[fact.UserField], &next) != nil || next == nil || json.Unmarshal(stored[fact.UserField], &old) != nil || old == nil {
			return credentials.ErrInvalid
		}
		// An unchanged reference is checked against the owner below. Other
		// mutation intents must not create future active state on this provider.
		if raw := next[fact.SecretField]; !bytes.Equal(raw, old[fact.SecretField]) {
			var intent struct {
				Intent string `json:"intent"`
			}
			if json.Unmarshal(raw, &intent) != nil || intent.Intent != "unchanged" {
				return common.NewError("CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE")
			}
		}
		delete(next, fact.SecretField)
		delete(old, fact.SecretField)
		var nextFields, oldFields map[string]any
		nextJSON, _ := json.Marshal(next)
		oldJSON, _ := json.Marshal(old)
		if json.Unmarshal(nextJSON, &nextFields) != nil || json.Unmarshal(oldJSON, &oldFields) != nil || !reflect.DeepEqual(nextFields, oldFields) {
			return common.NewError("CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE")
		}
	}
	if !editing {
		// Reserve an actual SQLite identity without ever writing submitted
		// plaintext credentials or delivery links. The final save and all
		// credential writes share the caller's transaction and rollback.
		placeholder := *client
		placeholder.Config, placeholder.Links = json.RawMessage(`{}`), json.RawMessage(`[]`)
		if err := tx.Create(&placeholder).Error; err != nil {
			return err
		}
		client.Id = placeholder.Id
	}
	for _, fact := range managed {
		var next, old map[string]json.RawMessage
		if json.Unmarshal(proposed[fact.UserField], &next) != nil || next == nil {
			return credentials.ErrInvalid
		}
		if entitycapabilities.Resolve("inbounds", fact.Type).Available {
			// These protocol schemas authenticate the Users principal. The
			// principal is derived by its owner, not a second identity editor.
			for key := range next {
				if key != "name" && key != fact.SecretField {
					return credentials.ErrInvalid
				}
			}
			next["name"], _ = json.Marshal(client.Name)
		}
		if len(stored[fact.UserField]) != 0 && json.Unmarshal(stored[fact.UserField], &old) != nil {
			return credentials.ErrInvalid
		}
		var captured string
		if raw := old[fact.SecretField]; len(raw) != 0 && json.Unmarshal(raw, &captured) != nil {
			return credentials.ErrInvalid
		}
		mutation, supplied := next[fact.SecretField]
		if !supplied && captured == "" && assigned[fact.Type] {
			value, err := credentials.Generate(fact.Type)
			if err != nil {
				return credentials.ErrInvalid
			}
			mutation, _ = json.Marshal(map[string]string{"intent": "replace", "value": value})
			supplied = true
		}
		if !supplied {
			if captured != "" {
				next[fact.SecretField], _ = json.Marshal(captured)
			}
		} else {
			var retained string
			// Bulk owner paths can retain their own captured envelope. This is
			// exact equality, never authority to submit a new opaque ciphertext.
			if json.Unmarshal(mutation, &retained) != nil || len(old[fact.SecretField]) == 0 || retained != captured || retained != "" && !secretbox.IsEncrypted(retained) {
				sealed, err := credentials.ApplyClientField(tx, client.Id, fact.Type, captured, mutation)
				if err != nil {
					return err
				}
				next[fact.SecretField], _ = json.Marshal(sealed)
			}
		}
		if assigned[fact.Type] {
			var value string
			if json.Unmarshal(next[fact.SecretField], &value) != nil {
				return credentials.ErrInvalid
			}
			if value == "" {
				var intent struct {
					Intent string `json:"intent"`
				}
				var retained string
				keepEmpty := json.Unmarshal(mutation, &intent) == nil && intent.Intent == "unchanged" ||
					len(old[fact.SecretField]) > 0 && json.Unmarshal(mutation, &retained) == nil && retained == "" && captured == ""
				if !keepEmpty {
					return credentials.ErrInvalid
				}
				generated, err := credentials.Generate(fact.Type)
				if err != nil {
					return credentials.ErrInvalid
				}
				replace, _ := json.Marshal(map[string]string{"intent": "replace", "value": generated})
				sealed, err := credentials.ApplyClientField(tx, client.Id, fact.Type, "", replace)
				if err != nil {
					return err
				}
				next[fact.SecretField], _ = json.Marshal(sealed)
			}
		}
		proposed[fact.UserField], _ = json.Marshal(next)
	}
	var err error
	client.Config, err = json.Marshal(proposed)
	return err
}

// PrepareAssignedCredentials is used by the existing inbound assignment and
// edit hooks. Its caller owns the enclosing inbound/client transaction.
func PrepareAssignedCredentials(tx *gorm.DB, client *model.Client) error {
	return prepareManagedCredentials(tx, client, true)
}
