package users

import (
	"bytes"
	"encoding/json"
	entityorder "github.com/MalenkiySolovey/solovey-ui/backend/storage/entityorder"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

func InboundsByID(tx *gorm.DB, id uint) ([]uint, error) {
	var client model.Client
	result := tx.Where("id = ?", id).Limit(1).Find(&client)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return []uint{}, nil
	}
	var inboundIDs []uint
	if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}

// FillOmittedContents hydrates a partial bulk-editor payload before validation
// and credential preparation. Explicit content remains owned by the save.
func FillOmittedContents(tx *gorm.DB, client *model.Client) error {
	var stored model.Client
	if err := tx.Select("config", "links").Where("id = ?", client.Id).First(&stored).Error; err != nil {
		return err
	}
	if len(client.Config) == 0 {
		client.Config = stored.Config
	}
	if len(client.Links) == 0 {
		client.Links = stored.Links
	}
	return nil
}

func FindInboundChanges(tx *gorm.DB, client *model.Client) ([]uint, error) {
	var oldClient model.Client
	var oldInboundIDs, newInboundIDs []uint
	if err := tx.Model(model.Client{}).Where("id = ?", client.Id).First(&oldClient).Error; err != nil {
		return nil, err
	}
	if err := json.Unmarshal(oldClient.Inbounds, &oldInboundIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(client.Inbounds, &newInboundIDs); err != nil {
		return nil, err
	}
	if !bytes.Equal(oldClient.Config, client.Config) ||
		oldClient.Name != client.Name ||
		oldClient.Enable != client.Enable {
		return unionInboundIDs(oldInboundIDs, newInboundIDs), nil
	}
	return differentInboundIDs(oldInboundIDs, newInboundIDs), nil
}
func DecodeInbounds(id uint, raw json.RawMessage, operation string, warningf func(string, ...any)) ([]uint, bool) {
	return DecodeInboundMembership(id, raw, operation, warningf)
}
func AppendInbound(clientID uint, raw json.RawMessage, inboundID uint, operation string, warningf func(string, ...any)) (json.RawMessage, bool, error) {
	inboundIDs, ok := DecodeInbounds(clientID, raw, operation, warningf)
	if !ok {
		return nil, false, nil
	}
	inboundIDs = append(inboundIDs, inboundID)
	marshaled, err := json.MarshalIndent(inboundIDs, "", "  ")
	if err != nil {
		return nil, true, err
	}
	return marshaled, true, nil
}
func RemoveInbound(clientID uint, raw json.RawMessage, inboundID uint, operation string, warningf func(string, ...any)) (json.RawMessage, bool, error) {
	inboundIDs, ok := DecodeInbounds(clientID, raw, operation, warningf)
	if !ok {
		return nil, false, nil
	}
	var nextInboundIDs []uint
	for _, existing := range inboundIDs {
		if existing != inboundID {
			nextInboundIDs = append(nextInboundIDs, existing)
		}
	}
	marshaled, err := json.MarshalIndent(nextInboundIDs, "", "  ")
	if err != nil {
		return nil, true, err
	}
	return marshaled, true, nil
}
func IDsByInbound(tx *gorm.DB, inboundID uint) ([]uint, error) {
	var clientIDs []uint
	err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ? ORDER BY clients.sort_order, clients.id", inboundID).Scan(&clientIDs).Error
	return clientIDs, err
}
func ByInbound(tx *gorm.DB, inboundID uint) ([]model.Client, error) {
	clientIDs, err := IDsByInbound(tx, inboundID)
	if err != nil || len(clientIDs) == 0 {
		return nil, err
	}
	var clients []model.Client
	err = tx.Model(model.Client{}).Where("id IN ?", clientIDs).Order(entityorder.Clause).Find(&clients).Error
	return clients, err
}

type InboundNameRow struct {
	InboundID uint
	Name      string
}

func NamesByInboundIDs(db *gorm.DB, inboundIDs []uint) (map[uint][]string, error) {
	usersByInbound := make(map[uint][]string, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return usersByInbound, nil
	}
	for _, id := range inboundIDs {
		usersByInbound[id] = []string{}
	}
	var rows []InboundNameRow
	err := db.Raw(`
		SELECT je.value AS inbound_id, clients.name
		FROM clients, json_each(clients.inbounds) AS je
		WHERE je.value IN ?
		ORDER BY clients.sort_order, clients.id, je.key
	`, inboundIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		usersByInbound[row.InboundID] = append(usersByInbound[row.InboundID], row.Name)
	}
	return usersByInbound, nil
}

func differentInboundIDs(a, b []uint) []uint {
	different := []uint{}
	set := make(map[uint]bool)
	for _, item := range a {
		set[item] = true
	}
	for _, item := range b {
		if !set[item] {
			different = append(different, item)
		}
	}
	set = make(map[uint]bool)
	for _, item := range b {
		set[item] = true
	}
	for _, item := range a {
		if !set[item] {
			different = append(different, item)
		}
	}
	return different
}
