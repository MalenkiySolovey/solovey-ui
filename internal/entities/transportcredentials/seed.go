// Package transportcredentials adapts fixed nested transport fields to the
// existing Inbounds/Outbounds SecretRef owners. It has no persistence owner.
package transportcredentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

const seedField = "transport.seed"

var ErrInvalid = errors.New("TRANSPORT_CREDENTIAL_INVALID")

func IsMKCP(raw []byte) bool {
	var header struct {
		Transport struct{ Type string }
	}
	return json.Unmarshal(raw, &header) == nil && header.Transport.Type == "mkcp"
}

type seedOptions struct {
	root, transport map[string]json.RawMessage
}

func readSeed(raw []byte) (*seedOptions, error) {
	var root, transport map[string]json.RawMessage
	if len(raw) == 0 {
		return nil, nil
	}
	if json.Unmarshal(raw, &root) != nil {
		return nil, ErrInvalid
	}
	var kind string
	if json.Unmarshal(root["transport"], &transport) != nil || json.Unmarshal(transport["type"], &kind) != nil || kind != "mkcp" {
		return nil, nil
	}
	if jsonvalue.StrictObject(raw) != nil || jsonvalue.StrictObject(root["transport"]) != nil {
		return nil, ErrInvalid
	}
	for key := range root {
		if key != "transport" && strings.EqualFold(key, "transport") {
			return nil, ErrInvalid
		}
	}
	for key := range transport {
		if (key != "seed" && strings.EqualFold(key, "seed")) || (key != "type" && strings.EqualFold(key, "type")) {
			return nil, ErrInvalid
		}
	}
	return &seedOptions{root: root, transport: transport}, nil
}

func (o *seedOptions) captured() (json.RawMessage, string) {
	raw := o.transport["seed"]
	var value string
	if len(raw) != 0 && json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	return raw, value
}

func (o *seedOptions) marshal() ([]byte, error) {
	var err error
	o.root["transport"], err = json.Marshal(o.transport)
	if err != nil {
		return nil, err
	}
	return json.Marshal(o.root)
}

// Prepare keeps row allocation in the caller's existing owner transaction.
// The caller supplies previous options only after matching the stored type.
func Prepare(tx *gorm.DB, owner string, rowID uint, raw, previous json.RawMessage, reserve func() (uint, error)) ([]byte, uint, error) {
	next, err := readSeed(raw)
	if err != nil || next == nil {
		return raw, rowID, err
	}
	var oldRaw json.RawMessage
	var captured string
	old, err := readSeed(previous)
	if err != nil {
		return nil, rowID, err
	}
	if old != nil {
		oldRaw, captured = old.captured()
	}
	mutation, supplied := next.transport["seed"]
	if !supplied {
		if len(oldRaw) != 0 {
			next.transport["seed"] = oldRaw
		}
	} else if len(oldRaw) == 0 || !bytes.Equal(mutation, oldRaw) {
		var intent struct{ Intent string }
		if jsonvalue.StrictObject(mutation) != nil || json.Unmarshal(mutation, &intent) != nil {
			return nil, rowID, ErrInvalid
		}
		if rowID == 0 {
			if intent.Intent == "clear" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(mutation, &fields) != nil || len(fields) != 1 {
					return nil, 0, ErrInvalid
				}
				delete(next.transport, "seed")
				value, err := next.marshal()
				return value, 0, err
			}
			if intent.Intent != "replace" || reserve == nil {
				return nil, 0, ErrInvalid
			}
			rowID, err = reserve()
			if err != nil {
				return nil, 0, err
			}
		}
		ref, err := reference(owner, rowID)
		if err != nil {
			return nil, rowID, err
		}
		sealed, err := ownerdb.Apply(tx, ref, captured, mutation)
		if err != nil {
			return nil, rowID, err
		}
		if sealed == "" {
			delete(next.transport, "seed")
		} else if sealed == captured && len(oldRaw) != 0 {
			next.transport["seed"] = oldRaw
		} else {
			next.transport["seed"], _ = json.Marshal(sealed)
		}
	}
	value, err := next.marshal()
	return value, rowID, err
}

func reference(owner string, rowID uint) (secretref.Reference, error) {
	if owner != "inbounds" && owner != "outbounds" {
		return secretref.Reference{}, ErrInvalid
	}
	return ownerdb.Reference(owner, rowID, seedField)
}

// Mask also serves the generated outbound template: it stores a reference to
// the canonical inbound seed rather than copying its ciphertext or plaintext.
func Mask(owner string, rowID uint, raw []byte) ([]byte, error) {
	options, err := readSeed(raw)
	if err != nil || options == nil {
		return raw, err
	}
	_, captured := options.captured()
	if captured == "" {
		return raw, nil
	}
	ref, err := reference(owner, rowID)
	if err != nil {
		return nil, err
	}
	options.transport["seed"], err = json.Marshal(ownerdb.View(ref, captured))
	if err != nil {
		return nil, err
	}
	return options.marshal()
}

func Runtime(db *gorm.DB, owner string, rowID uint, raw []byte) ([]byte, error) {
	return resolve(db, owner, rowID, raw, "transport-runtime-config")
}

// Delivery resolves the canonical inbound field into an ephemeral client
// template after the subscription owner has checked client assignment.
func Delivery(db *gorm.DB, rowID uint, canonical, template []byte) ([]byte, error) {
	resolved, err := resolve(db, "inbounds", rowID, canonical, "transport-client-delivery")
	if err != nil {
		return nil, err
	}
	options, err := readSeed(resolved)
	if err != nil || options == nil {
		return template, err
	}
	_, captured := options.captured()
	if captured == "" {
		return template, nil
	}
	target, err := readSeed(template)
	if err != nil || target == nil {
		return nil, ErrInvalid
	}
	target.transport["seed"] = options.transport["seed"]
	return target.marshal()
}

func resolve(db *gorm.DB, owner string, rowID uint, raw []byte, intent string) ([]byte, error) {
	options, err := readSeed(raw)
	if err != nil || options == nil {
		return raw, err
	}
	_, captured := options.captured()
	if captured == "" {
		return raw, nil
	}
	if !secretbox.IsEncrypted(captured) {
		return nil, ErrInvalid
	}
	ref, err := reference(owner, rowID)
	if err != nil {
		return nil, err
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: owner}, Operation: secretref.UseForOwner, IntentID: intent}
	_, err = ownerdb.Access(db.Statement.Context, db, request, captured, func(value []byte) error {
		options.transport["seed"], _ = json.Marshal(string(value))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return options.marshal()
}
