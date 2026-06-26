package entityclients

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
	"strings"
)

type SaveAction string

const (
	ActionNew      SaveAction = "new"
	ActionEdit     SaveAction = "edit"
	ActionAddBulk  SaveAction = "addbulk"
	ActionEditBulk SaveAction = "editbulk"
	ActionDelBulk  SaveAction = "delbulk"
	ActionDel      SaveAction = "del"
)

var supportedSaveActions = []SaveAction{
	ActionNew,
	ActionEdit,
	ActionAddBulk,
	ActionEditBulk,
	ActionDelBulk,
	ActionDel,
}

type SaveRequest struct {
	Tx       *gorm.DB
	Action   string
	Data     json.RawMessage
	Hostname string
}
type Link map[string]any

func Get(db *gorm.DB, id string) (*[]model.Client, error) {
	if id == "" {
		return GetAll(db)
	}
	return GetByID(db, id)
}
func GetWithLocalLinks(db *gorm.DB, id string, hostname string) (*[]model.Client, error) {
	clients, err := Get(db, id)
	if err != nil || id == "" {
		return clients, err
	}
	if err := PreviewWithLocalLinks(db, clients, hostname); err != nil {
		return nil, err
	}
	return clients, nil
}
func GetByID(db *gorm.DB, id string) (*[]model.Client, error) {
	var client []model.Client
	err := db.Model(model.Client{}).Where("id in ?", strings.Split(id, ",")).Order(entityorder.Clause).Scan(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}
func GetAll(db *gorm.DB) (*[]model.Client, error) {
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select("`id`, `sort_order`, `enable`, `name`, `sub_secret`, `desc`, `group`, `inbounds`, `up`, `down`, `volume`, `expiry`, `limit_ip`, `ip_limit_mode`, `last_online`, `last_ip_count`").
		Order(entityorder.Clause).
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	return &clients, nil
}
func Save(req SaveRequest) ([]uint, error) {
	action, ok := ParseAction(req.Action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.Action)
	}
	switch action {
	case ActionNew:
		return saveSingle(req, false)
	case ActionEdit:
		return saveSingle(req, true)
	case ActionAddBulk:
		return saveAddedBulk(req)
	case ActionEditBulk:
		return saveEditedBulk(req)
	case ActionDelBulk:
		return saveDeletedBulk(req)
	case ActionDel:
		return saveDeleted(req)
	default:
		return nil, common.NewErrorf("unknown action: %s", req.Action)
	}
}
func ParseAction(action string) (SaveAction, bool) {
	saveAction := SaveAction(action)
	for _, supported := range supportedSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}
func SupportedActionStrings() []string {
	actions := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}
func saveSingle(req SaveRequest, editing bool) ([]uint, error) {
	var client model.Client
	if err := json.Unmarshal(req.Data, &client); err != nil {
		return nil, err
	}
	if err := PrepareSubSecret(req.Tx, &client, editing); err != nil {
		return nil, err
	}
	if err := UpdateLinksWithFixedInbounds(req.Tx, []*model.Client{&client}, req.Hostname); err != nil {
		return nil, err
	}
	sortOrder, err := entityorder.ForSave(req.Tx, &model.Client{}, client.Id)
	if err != nil {
		return nil, err
	}
	client.SortOrder = sortOrder
	var inboundIDs []uint
	if editing {
		changedInboundIDs, err := FindInboundChanges(req.Tx, &client, false)
		if err != nil {
			return nil, err
		}
		inboundIDs = changedInboundIDs
	} else if err := json.Unmarshal(client.Inbounds, &inboundIDs); err != nil {
		return nil, err
	}
	if err := req.Tx.Save(&client).Error; err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func saveAddedBulk(req SaveRequest) ([]uint, error) {
	var clients []*model.Client
	if err := json.Unmarshal(req.Data, &clients); err != nil {
		return nil, err
	}
	var inboundIDs []uint
	if len(clients) == 0 {
		return inboundIDs, nil
	}
	if err := json.Unmarshal(clients[0].Inbounds, &inboundIDs); err != nil {
		return nil, err
	}
	for _, client := range clients {
		if err := PrepareSubSecret(req.Tx, client, false); err != nil {
			return nil, err
		}
	}
	nextOrder, err := entityorder.Next(req.Tx, &model.Client{})
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		client.SortOrder = nextOrder
		nextOrder++
	}
	if err := UpdateLinksWithFixedInbounds(req.Tx, clients, req.Hostname); err != nil {
		return nil, err
	}
	if err := dbsqlite.SaveInBatches(req.Tx, clients); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func saveEditedBulk(req SaveRequest) ([]uint, error) {
	var clients []*model.Client
	if err := json.Unmarshal(req.Data, &clients); err != nil {
		return nil, err
	}
	var inboundIDs []uint
	for _, client := range clients {
		if err := PrepareSubSecret(req.Tx, client, true); err != nil {
			return nil, err
		}
		changedInboundIDs, err := FindInboundChanges(req.Tx, client, true)
		if err != nil {
			return nil, err
		}
		if len(changedInboundIDs) > 0 {
			inboundIDs = common.UnionUintArray(inboundIDs, changedInboundIDs)
		}
		sortOrder, err := entityorder.ForSave(req.Tx, &model.Client{}, client.Id)
		if err != nil {
			return nil, err
		}
		client.SortOrder = sortOrder
	}
	if len(inboundIDs) > 0 {
		if err := UpdateLinksWithFixedInbounds(req.Tx, clients, req.Hostname); err != nil {
			return nil, err
		}
	}
	if err := dbsqlite.SaveInBatches(req.Tx, clients); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func saveDeletedBulk(req SaveRequest) ([]uint, error) {
	var ids []uint
	if err := json.Unmarshal(req.Data, &ids); err != nil {
		return nil, err
	}
	var inboundIDs []uint
	for _, id := range ids {
		clientInbounds, err := InboundsByID(req.Tx, id)
		if err != nil {
			return nil, err
		}
		inboundIDs = common.UnionUintArray(inboundIDs, clientInbounds)
	}
	if err := req.Tx.Where("id in ?", ids).Delete(model.Client{}).Error; err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func saveDeleted(req SaveRequest) ([]uint, error) {
	var id uint
	if err := json.Unmarshal(req.Data, &id); err != nil {
		return nil, err
	}
	inboundIDs, err := InboundsByID(req.Tx, id)
	if err != nil {
		return nil, err
	}
	if err := req.Tx.Where("id = ?", id).Delete(model.Client{}).Error; err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func PrepareSubSecret(tx *gorm.DB, client *model.Client, preserveExisting bool) error {
	if client.IPLimitMode == "" {
		client.IPLimitMode = "monitor"
	}
	if client.SubSecret != "" {
		return nil
	}
	if preserveExisting && client.Id > 0 {
		var old model.Client
		if err := tx.Model(model.Client{}).Select("sub_secret").Where("id = ?", client.Id).First(&old).Error; err != nil {
			return err
		}
		if old.SubSecret != "" {
			client.SubSecret = old.SubSecret
			return nil
		}
	}
	secret, err := common.RandomUUID()
	if err != nil {
		return err
	}
	client.SubSecret = secret
	return nil
}
