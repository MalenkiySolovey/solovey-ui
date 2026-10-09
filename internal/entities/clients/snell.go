package entityclients

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

// PrepareSnellCredential is called only from the existing client mutation
// lifecycle. Missing keys are initialized once on attachment; an existing key
// is retained, including when an older client editor omits the protocol map.
// Explicit invalid edits fail instead of rotating a credential silently.
func PrepareSnellCredential(tx *gorm.DB, client *model.Client) error {
	var config map[string]json.RawMessage
	if len(client.Config) > 0 && json.Unmarshal(client.Config, &config) != nil {
		return errors.New("client credential config is invalid")
	}
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	var previous model.Client
	if client.Id != 0 {
		if err := tx.Select("config").Where("id = ?", client.Id).Find(&previous).Error; err != nil {
			return err
		}
	}
	var stored map[string]json.RawMessage
	_ = json.Unmarshal(previous.Config, &stored)
	if _, supplied := config["snell"]; !supplied && len(stored["snell"]) > 0 {
		config["snell"] = stored["snell"]
	}
	var ids []uint
	if err := json.Unmarshal(client.Inbounds, &ids); err != nil && len(client.Inbounds) > 0 {
		return err
	}
	var count int64
	if len(ids) > 0 {
		if err := tx.Model(&model.Inbound{}).Where("id IN ? AND type = ?", ids, "snell").Count(&count).Error; err != nil {
			return err
		}
	}
	if count == 0 && len(config["snell"]) == 0 {
		return nil
	}
	var user map[string]any
	if len(config["snell"]) > 0 {
		if json.Unmarshal(config["snell"], &user) != nil || user == nil {
			return errors.New("snell client credential must be an object")
		}
	} else {
		user = map[string]any{}
	}
	for field := range user {
		if field != "name" && field != "userkey" {
			return errors.New("unsupported Snell client credential field")
		}
	}
	key, supplied := user["userkey"]
	if !supplied {
		var oldUser map[string]any
		_ = json.Unmarshal(stored["snell"], &oldUser)
		if previousKey, exists := oldUser["userkey"]; exists {
			key = previousKey
		} else {
			value, err := common.SecureRandom(32)
			if err != nil {
				return errors.New("snell credential generation failed")
			}
			key = value
		}
	}
	value, ok := key.(string)
	if !ok || len(value) == 0 || len(value) > registry.SnellContract("inbounds").UserKeyMaxBytes {
		return errors.New("snell user key must contain 1 to 255 bytes")
	}
	user["userkey"], user["name"] = value, client.Name
	config["snell"], _ = json.Marshal(user)
	var err error
	client.Config, err = json.Marshal(config)
	return err
}

// A user key identifies one client in an official multi-user inbound. Check the
// complete candidate batch together with persisted memberships before saving.
// Reusing a key on disjoint inbounds is safe; sharing it on one inbound is not.
func validateSnellUserKeys(tx *gorm.DB, candidates []*model.Client) error {
	var inboundIDs []uint
	if err := tx.Model(&model.Inbound{}).Where("type = ?", "snell").Pluck("id", &inboundIDs).Error; err != nil {
		return err
	}
	if len(inboundIDs) == 0 {
		return nil
	}
	snellIDs := map[uint]bool{}
	for _, id := range inboundIDs {
		snellIDs[id] = true
	}
	replaced := map[uint]bool{}
	for _, client := range candidates {
		if client.Id != 0 {
			replaced[client.Id] = true
		}
	}
	var stored []model.Client
	if err := tx.Select("id, config, inbounds").Find(&stored).Error; err != nil {
		return err
	}
	rows := append([]*model.Client{}, candidates...)
	for index := range stored {
		if !replaced[stored[index].Id] {
			rows = append(rows, &stored[index])
		}
	}
	seen := map[uint]map[string]bool{}
	for _, client := range rows {
		var ids []uint
		if err := json.Unmarshal(client.Inbounds, &ids); err != nil {
			continue
		}
		var config struct {
			Snell struct {
				UserKey string `json:"userkey"`
			} `json:"snell"`
		}
		decodeErr := json.Unmarshal(client.Config, &config)
		memberships := map[uint]bool{}
		for _, id := range ids {
			if !snellIDs[id] || memberships[id] {
				continue
			}
			memberships[id] = true
			if decodeErr != nil || config.Snell.UserKey == "" || len(config.Snell.UserKey) > registry.SnellContract("inbounds").UserKeyMaxBytes {
				return errors.New("attached Snell client requires a user key")
			}
			if seen[id] == nil {
				seen[id] = map[string]bool{}
			}
			if seen[id][config.Snell.UserKey] {
				return errors.New("snell clients on one inbound require unique user keys")
			}
			seen[id][config.Snell.UserKey] = true
		}
	}
	return nil
}
