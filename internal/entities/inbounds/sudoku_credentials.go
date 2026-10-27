package entityinbounds

import (
	"bytes"
	"encoding/hex"
	"encoding/json"

	"filippo.io/edwards25519"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func capturedSudokuMaster(options map[string]json.RawMessage) (json.RawMessage, string) {
	raw := options["master_key"]
	var captured string
	if len(raw) != 0 && json.Unmarshal(raw, &captured) != nil {
		captured = string(raw)
	}
	return raw, captured
}

func prepareSudokuCredentials(tx *gorm.DB, inbound *model.Inbound) error {
	var next, old map[string]json.RawMessage
	if jsonvalue.StrictObject(inbound.Options) != nil || json.Unmarshal(inbound.Options, &next) != nil || next == nil {
		return errInboundCredentialInvalid
	}
	if inbound.Id != 0 {
		var stored model.Inbound
		if err := tx.Select("id", "type", "options").First(&stored, inbound.Id).Error; err != nil {
			return err
		}
		if stored.Type == inbound.Type && json.Unmarshal(stored.Options, &old) != nil {
			return errInboundCredentialInvalid
		}
	}
	oldRaw, captured := capturedSudokuMaster(old)
	if _, supplied := next["key"]; !supplied && len(old["key"]) != 0 {
		next["key"] = old["key"]
	}
	mutation, supplied := next["master_key"]
	available := entitycapabilities.Resolve("inbounds", inbound.Type).Available
	if !supplied && inbound.Id == 0 && available {
		master, err := credentials.GenerateSudokuMaster()
		if err != nil {
			return err
		}
		mutation, _ = json.Marshal(map[string]string{"intent": "replace", "value": master})
		supplied = true
	}
	if supplied && !(inbound.Id != 0 && len(oldRaw) != 0 && bytes.Equal(mutation, oldRaw)) {
		var intent struct{ Intent, Value string }
		if json.Unmarshal(mutation, &intent) != nil || !available && intent.Intent != "unchanged" {
			return errInboundCredentialInvalid
		}
		if intent.Intent == "replace" {
			if _, err := credentials.SudokuPublicKey(intent.Value); err != nil {
				return errInboundCredentialInvalid
			}
		}
		if inbound.Id == 0 {
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
		ref, err := ownerdb.Reference("inbounds", inbound.Id, "master_key")
		if err != nil {
			return err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return err
		}
		if sealed == "" {
			delete(next, "master_key")
		} else if sealed == captured && len(oldRaw) != 0 {
			next["master_key"] = oldRaw
		} else {
			next["master_key"], _ = json.Marshal(sealed)
		}
	} else if len(oldRaw) != 0 {
		next["master_key"] = oldRaw
	}
	// The public key is derived owner state, never an independent key editor.
	if suppliedKey, present := next["key"]; present && len(old["key"]) != 0 && !bytes.Equal(suppliedKey, old["key"]) {
		return errInboundCredentialInvalid
	}
	inbound.Options, _ = json.Marshal(next)
	if !available {
		return nil
	}
	_, sealed := capturedSudokuMaster(next)
	if sealed != "" {
		if !secretbox.IsEncrypted(sealed) {
			return errInboundCredentialInvalid
		}
		err := credentials.UseSudokuMaster(tx.Statement.Context, tx, *inbound, "sudoku-public-key", func(value []byte) error {
			public, err := credentials.SudokuPublicKey(string(value))
			if err != nil {
				return err
			}
			if len(old["key"]) == 0 && len(next["key"]) != 0 {
				var proposed string
				if json.Unmarshal(next["key"], &proposed) != nil || proposed != public {
					return errInboundCredentialInvalid
				}
			}
			next["key"], _ = json.Marshal(public)
			return nil
		})
		if err != nil {
			return err
		}
	} else if !validSudokuPublic(next["key"]) {
		return errInboundCredentialInvalid
	}
	inbound.Options, _ = json.Marshal(next)
	return nil
}

func validSudokuPublic(raw json.RawMessage) bool {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	encoded, err := hex.DecodeString(value)
	if err != nil || len(encoded) != 32 {
		return false
	}
	point, err := edwards25519.NewIdentityPoint().SetBytes(encoded)
	return err == nil && bytes.Equal(point.Bytes(), encoded) && edwards25519.NewIdentityPoint().MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) != 1
}

func projectSudokuCredentials(inbound model.Inbound, data *map[string]interface{}) error {
	var options map[string]json.RawMessage
	if json.Unmarshal(inbound.Options, &options) != nil {
		return errInboundCredentialInvalid
	}
	_, captured := capturedSudokuMaster(options)
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "master_key")
	if err != nil {
		return err
	}
	(*data)["master_key"] = ownerdb.View(ref, captured)
	return nil
}

func marshalSudokuRuntime(db *gorm.DB, inbound model.Inbound, raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &fields) != nil || !validSudokuPublic(fields["key"]) {
		return nil, errInboundCredentialInvalid
	}
	_, captured := capturedSudokuMaster(fields)
	if captured != "" {
		if err := credentials.UseSudokuMaster(db.Statement.Context, db, inbound, "inbound-config", func(value []byte) error {
			public, err := credentials.SudokuPublicKey(string(value))
			var expected string
			if err != nil || json.Unmarshal(fields["key"], &expected) != nil || expected != public {
				return errInboundCredentialInvalid
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	delete(fields, "master_key")
	return json.Marshal(fields)
}
