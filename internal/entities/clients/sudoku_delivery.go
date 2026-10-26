package entityclients

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func prepareSudokuDelivery(tx *gorm.DB, client *model.Client, editing bool) error {
	var root, stored map[string]json.RawMessage
	if len(client.Config) != 0 && json.Unmarshal(client.Config, &root) != nil {
		return credentials.ErrInvalid
	}
	if root == nil {
		root = map[string]json.RawMessage{}
	}
	var old model.Client
	if editing {
		if err := tx.Select("config", "inbounds").First(&old, client.Id).Error; err != nil {
			return err
		}
		if len(old.Config) != 0 && json.Unmarshal(old.Config, &stored) != nil {
			return credentials.ErrInvalid
		}
	}
	var ids, oldIDs []uint
	if len(client.Inbounds) != 0 && json.Unmarshal(client.Inbounds, &ids) != nil || len(old.Inbounds) != 0 && json.Unmarshal(old.Inbounds, &oldIDs) != nil {
		return credentials.ErrInvalid
	}
	var inbounds []model.Inbound
	if len(ids) != 0 {
		if err := tx.Select("id", "type", "options").Where("id IN ? AND type = ?", ids, "sudoku").Find(&inbounds).Error; err != nil {
			return err
		}
	}
	if len(root["sudoku"]) == 0 {
		root["sudoku"] = stored["sudoku"]
	}
	if len(inbounds) == 0 && len(root["sudoku"]) == 0 {
		return nil
	}
	if len(client.Config) != 0 && jsonvalue.StrictObject(client.Config) != nil || len(old.Config) != 0 && jsonvalue.StrictObject(old.Config) != nil {
		return credentials.ErrInvalid
	}
	var next, previous map[string]json.RawMessage
	if len(root["sudoku"]) != 0 && json.Unmarshal(root["sudoku"], &next) != nil || len(stored["sudoku"]) != 0 && json.Unmarshal(stored["sudoku"], &previous) != nil {
		return credentials.ErrInvalid
	}
	if next == nil {
		next = map[string]json.RawMessage{}
	}
	available := entitycapabilities.Resolve("inbounds", "sudoku").Available
	oldMembership := map[uint]bool{}
	for _, id := range oldIDs {
		oldMembership[id] = true
	}
	if !available {
		if !editing || len(stored["sudoku"]) == 0 {
			return common.NewError("CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE")
		}
		for _, inbound := range inbounds {
			if !oldMembership[inbound.Id] {
				return credentials.ErrInvalid
			}
		}
		for key, value := range next {
			if key != "keys" && !bytes.Equal(value, previous[key]) {
				return credentials.ErrInvalid
			}
		}
		for key := range previous {
			if key != "keys" && !bytes.Equal(next[key], previous[key]) {
				return credentials.ErrInvalid
			}
		}
	} else {
		for key := range next {
			if key != "keys" {
				return credentials.ErrInvalid
			}
		}
	}
	var captured string
	if len(previous["keys"]) != 0 && json.Unmarshal(previous["keys"], &captured) != nil {
		captured = string(previous["keys"])
	}
	mutation, supplied := next["keys"]
	var intent struct{ Intent, Value string }
	exact := supplied && len(previous["keys"]) != 0 && bytes.Equal(mutation, previous["keys"])
	if supplied && !exact && json.Unmarshal(mutation, &intent) != nil {
		return credentials.ErrInvalid
	}
	if !available && supplied && !exact && intent.Intent != "unchanged" {
		return credentials.ErrInvalid
	}
	if client.Id == 0 {
		placeholder := *client
		placeholder.Config, placeholder.Links = json.RawMessage(`{}`), json.RawMessage(`[]`)
		if err := tx.Create(&placeholder).Error; err != nil {
			return err
		}
		client.Id = placeholder.Id
	}
	ref, err := ownerdb.Reference("users", client.Id, "sudoku.keys")
	if err != nil {
		return err
	}
	sealed := captured
	keys := map[string]string{}
	if supplied && !exact && intent.Intent == "replace" {
		if !available {
			return credentials.ErrInvalid
		}
		keys, err = credentials.SudokuKeyBag(intent.Value)
		if err != nil {
			return err
		}
	} else {
		if supplied && !exact {
			sealed, err = ownerdb.Apply(tx, ref, captured, mutation)
			if err != nil {
				return err
			}
		}
		if sealed != "" && available {
			request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "users"}, Operation: secretref.UseForOwner, IntentID: "sudoku-delivery-keys"}
			_, err = ownerdb.Access(tx.Statement.Context, tx, request, sealed, func(value []byte) error {
				var err error
				keys, err = credentials.SudokuKeyBag(string(value))
				return err
			})
			if err != nil {
				return err
			}
		}
	}
	before := make(map[string]string, len(keys))
	for id, key := range keys {
		before[id] = key
	}
	if available && intent.Intent != "clear" {
		for _, inbound := range inbounds {
			id := strconv.FormatUint(uint64(inbound.Id), 10)
			// A cleared existing bundle remains absent. A new assignment or
			// explicit refresh may initialize it; unassignment never erases keys.
			if keys[id] == "" && sealed == "" && len(previous["keys"]) != 0 && oldMembership[inbound.Id] && intent.Intent != "replace" {
				continue
			}
			if keys[id] != "" {
				var serving struct {
					Key string `json:"key"`
				}
				public, err := credentials.SudokuSplitPublicKey(keys[id])
				if err == nil && json.Unmarshal(inbound.Options, &serving) == nil && public == serving.Key {
					continue
				}
				if intent.Intent == "replace" {
					return credentials.ErrInvalid
				}
			}
			err := credentials.UseSudokuMaster(tx.Statement.Context, tx, inbound, "sudoku-client-split", func(master []byte) error {
				var err error
				keys[id], err = credentials.SplitSudokuMaster(string(master))
				return err
			})
			if err != nil {
				return err
			}
		}
	}
	if available && (intent.Intent == "replace" || !reflect.DeepEqual(before, keys)) {
		value, _ := json.Marshal(keys)
		if len(value) > 1<<20 {
			return credentials.ErrInvalid
		}
		replace, _ := json.Marshal(map[string]string{"intent": "replace", "value": string(value)})
		sealed, err = ownerdb.Apply(tx, ref, sealed, replace)
		if err != nil {
			return err
		}
	}
	if sealed == captured && len(previous["keys"]) != 0 {
		next["keys"] = previous["keys"]
	} else {
		next["keys"], _ = json.Marshal(sealed)
	}
	root["sudoku"], _ = json.Marshal(next)
	client.Config, err = json.Marshal(root)
	return err
}
