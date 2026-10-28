package entityinbounds

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/transportcredentials"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

var errInboundCredentialInvalid = errors.New("INBOUND_CREDENTIAL_INVALID")

func managedNativeInbound(kind string) bool {
	if kind == "sudoku" || kind == "call" {
		return true
	}
	fact, known := clientfacts.Resolve(kind)
	return known && fact.SecretField != ""
}

// A host-key bundle remains in its existing Inbound.Options row. Only the
// common SecretRef adapter can seal or borrow it; this adds no key/store owner.
func capturedHostKey(options map[string]json.RawMessage) (json.RawMessage, string) {
	raw := options["host_key"]
	var captured string
	if len(raw) != 0 && json.Unmarshal(raw, &captured) != nil {
		// Historical unsupported material remains masked and preservable. It
		// cannot acquire runtime authority without an explicit replacement.
		captured = string(raw)
	}
	return raw, captured
}

func sshHostKeys(bundle []byte) ([]string, error) {
	var keys []string
	remaining := bytes.TrimSpace(bundle)
	for len(remaining) != 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN ")) {
			return nil, errInboundCredentialInvalid
		}
		block, rest := pem.Decode(remaining)
		if block == nil || !strings.HasSuffix(block.Type, "PRIVATE KEY") {
			return nil, errInboundCredentialInvalid
		}
		key := pem.EncodeToMemory(block)
		if _, err := ssh.ParsePrivateKey(key); err != nil {
			return nil, errInboundCredentialInvalid
		}
		keys = append(keys, string(key))
		remaining = bytes.TrimSpace(rest)
	}
	if len(keys) == 0 {
		return nil, errInboundCredentialInvalid
	}
	return keys, nil
}

func prepareInboundCredentials(tx *gorm.DB, inbound *model.Inbound) error {
	if inbound.Type == "call" {
		return prepareCallCredentials(tx, inbound)
	}
	if inbound.Type == "sudoku" {
		return prepareSudokuCredentials(tx, inbound)
	}
	if inbound.Type != "ssh" {
		return nil
	}
	var next, old map[string]json.RawMessage
	if len(inbound.Options) != 0 && (jsonvalue.StrictObject(inbound.Options) != nil || json.Unmarshal(inbound.Options, &next) != nil) {
		return errInboundCredentialInvalid
	}
	if next == nil {
		next = map[string]json.RawMessage{}
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
	oldRaw, captured := capturedHostKey(old)
	mutation, supplied := next["host_key"]
	if !supplied {
		if len(oldRaw) != 0 {
			next["host_key"] = oldRaw
		}
		inbound.Options, _ = json.Marshal(next)
		return nil
	}
	// Trusted bulk paths may retain the exact captured field. This grants no
	// authority to submit another row's envelope or an arbitrary ciphertext.
	if inbound.Id != 0 && len(oldRaw) != 0 && bytes.Equal(mutation, oldRaw) {
		return nil
	}
	var intent struct{ Intent, Value string }
	if json.Unmarshal(mutation, &intent) != nil {
		return errInboundCredentialInvalid
	}
	if !entitycapabilities.Resolve("inbounds", inbound.Type).Available && intent.Intent != "unchanged" {
		return errors.New("NATIVE_INBOUND_CAPABILITY_UNAVAILABLE")
	}
	if intent.Intent == "replace" {
		if len(intent.Value) > 1<<20 {
			return errInboundCredentialInvalid
		}
		if _, err := sshHostKeys([]byte(intent.Value)); err != nil {
			return err
		}
	}
	if inbound.Id == 0 {
		if intent.Intent == "clear" {
			var fields map[string]json.RawMessage
			if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
				return errInboundCredentialInvalid
			}
			delete(next, "host_key")
			inbound.Options, _ = json.Marshal(next)
			return nil
		}
		if intent.Intent != "replace" {
			return errInboundCredentialInvalid
		}
		// Reserve a real row identity without writing private material. The
		// placeholder, common key/audit writes and final row share one save tx.
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
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "host_key")
	if err != nil {
		return err
	}
	sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
	if err != nil {
		return err
	}
	if sealed == "" {
		delete(next, "host_key")
	} else if sealed == captured && len(oldRaw) != 0 {
		next["host_key"] = oldRaw
	} else {
		next["host_key"], _ = json.Marshal(sealed)
	}
	inbound.Options, err = json.Marshal(next)
	return err
}

func projectInboundCredentials(inbound model.Inbound, data *map[string]interface{}) error {
	if transportcredentials.IsMKCP(inbound.Options) {
		raw, err := json.Marshal(*data)
		if err != nil {
			return err
		}
		masked, err := transportcredentials.Mask("inbounds", inbound.Id, raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(masked, data); err != nil {
			return err
		}
	}
	if inbound.Type == "call" {
		return projectCallCredentials(inbound, data)
	}
	if inbound.Type == "sudoku" {
		return projectSudokuCredentials(inbound, data)
	}
	if inbound.Type != "ssh" {
		return nil
	}
	var options map[string]json.RawMessage
	if len(inbound.Options) != 0 && json.Unmarshal(inbound.Options, &options) != nil {
		return errInboundCredentialInvalid
	}
	_, captured := capturedHostKey(options)
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "host_key")
	if err != nil {
		return err
	}
	(*data)["host_key"] = ownerdb.View(ref, captured)
	return nil
}

func marshalRuntimeInbound(db *gorm.DB, inbound model.Inbound) ([]byte, error) {
	runtimeInbound := inbound
	runtimeInbound.Type = entitytypes.RuntimeType("inbounds", inbound.Type)
	raw, err := runtimeInbound.MarshalJSON()
	if err == nil {
		raw, err = transportcredentials.Runtime(db, "inbounds", inbound.Id, raw)
	}
	if err == nil && inbound.Type == "call" {
		return marshalCallRuntime(db, inbound, raw)
	}
	if err == nil && inbound.Type == "sudoku" {
		return marshalSudokuRuntime(db, inbound, raw)
	}
	if err != nil || inbound.Type != "ssh" {
		return raw, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errInboundCredentialInvalid
	}
	_, captured := capturedHostKey(fields)
	if captured == "" {
		return raw, nil
	}
	if !secretbox.IsEncrypted(captured) {
		return nil, errInboundCredentialInvalid
	}
	ref, err := ownerdb.Reference("inbounds", inbound.Id, "host_key")
	if err != nil {
		return nil, err
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "inbounds"}, Operation: secretref.UseForOwner, IntentID: "inbound-config"}
	_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
		keys, err := sshHostKeys(value)
		if err != nil {
			return err
		}
		fields["host_key"], _ = json.Marshal(keys)
		raw, err = json.Marshal(fields)
		return err
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}
