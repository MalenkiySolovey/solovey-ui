package entityclients

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveidentity"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
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
	Tx        *gorm.DB
	Action    string
	Data      json.RawMessage
	Hostname  string
	SaveBatch func(tx *gorm.DB, slice any) error
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

// ValidateStored verifies subscription identities that entered through
// migration or restore instead of the client save authority.
func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("client persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Client{}) {
		return nil
	}
	var clients []struct {
		ID        uint
		SubSecret string
		Config    json.RawMessage
		Inbounds  json.RawMessage
		Links     json.RawMessage
	}
	if err := db.Model(&model.Client{}).Select("id", "sub_secret", "config", "inbounds", "links").Order("id").Scan(&clients).Error; err != nil {
		return fmt.Errorf("load stored client subscription identities: %w", err)
	}
	seen := make(map[string]uint, len(clients))
	for _, client := range clients {
		secret := strings.TrimSpace(client.SubSecret)
		if secret == "" || secret != client.SubSecret {
			return fmt.Errorf("stored client row %d has an invalid subscription secret", client.ID)
		}
		if previous, exists := seen[secret]; exists {
			return fmt.Errorf("stored client row %d duplicates subscription secret owned by client row %d", client.ID, previous)
		}
		seen[secret] = client.ID
		if err := jsonvalue.OptionalObject("client config", client.Config); err != nil {
			return fmt.Errorf("stored client row %d: %w", client.ID, err)
		}
		if err := jsonvalue.OptionalArray("client inbounds", client.Inbounds); err != nil {
			return fmt.Errorf("stored client row %d: %w", client.ID, err)
		}
		if err := jsonvalue.OptionalArray("client links", client.Links); err != nil {
			return fmt.Errorf("stored client row %d: %w", client.ID, err)
		}
	}
	return nil
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
	if err := validateClientSaveBatch(req.Tx, []*model.Client{&client}, editing); err != nil {
		return nil, err
	}
	if err := reconcileClientIPIdentity(req.Tx, &client, editing); err != nil {
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
	if err := validateClientSaveBatch(req.Tx, clients, false); err != nil {
		return nil, err
	}
	for _, client := range clients {
		if err := reconcileClientIPIdentity(req.Tx, client, false); err != nil {
			return nil, err
		}
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
	if req.SaveBatch == nil {
		return nil, common.NewError("client batch persistence is unavailable")
	}
	if err := req.SaveBatch(req.Tx, clients); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}
func saveEditedBulk(req SaveRequest) ([]uint, error) {
	var clients []*model.Client
	if err := json.Unmarshal(req.Data, &clients); err != nil {
		return nil, err
	}
	if err := validateClientSaveBatch(req.Tx, clients, true); err != nil {
		return nil, err
	}
	for _, client := range clients {
		if err := reconcileClientIPIdentity(req.Tx, client, true); err != nil {
			return nil, err
		}
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
	if req.SaveBatch == nil {
		return nil, common.NewError("client batch persistence is unavailable")
	}
	if err := req.SaveBatch(req.Tx, clients); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}

func validateClientSaveBatch(tx *gorm.DB, clients []*model.Client, editing bool) error {
	if tx == nil {
		return common.NewError("client database is unavailable")
	}
	if len(clients) == 0 {
		return nil
	}
	byName := make(map[string]uint, len(clients))
	names := make([]string, 0, len(clients))
	for _, client := range clients {
		if client == nil {
			return common.NewError("client payload is invalid")
		}
		client.Name = strings.TrimSpace(client.Name)
		if err := entityidentity.ValidateName(client.Name); err != nil {
			return err
		}
		if err := jsonvalue.OptionalObject("client config", client.Config); err != nil {
			return err
		}
		if err := jsonvalue.OptionalArray("client inbounds", client.Inbounds); err != nil {
			return err
		}
		if err := jsonvalue.OptionalArray("client links", client.Links); err != nil {
			return err
		}
		action := "new"
		if editing {
			action = "edit"
		}
		if err := saveidentity.Validate(tx, action, client.Id, &model.Client{}); err != nil {
			return err
		}
		if _, duplicate := byName[client.Name]; duplicate {
			return common.NewError("client name already exists")
		}
		byName[client.Name] = client.Id
		names = append(names, client.Name)
	}
	var existing []model.Client
	if err := tx.Model(model.Client{}).Select("id, name").Where("name IN ?", names).Find(&existing).Error; err != nil {
		return err
	}
	for _, stored := range existing {
		if byName[stored.Name] != stored.Id {
			return common.NewError("client name already exists")
		}
	}
	return nil
}

func reconcileClientIPIdentity(tx *gorm.DB, client *model.Client, editing bool) error {
	if !editing {
		// A deleted client's observations must never be inherited by a newly
		// created client that happens to reuse the same display name.
		return tx.Where("client_name = ?", client.Name).Delete(&model.ClientIP{}).Error
	}
	var oldName string
	if err := tx.Model(model.Client{}).Select("name").Where("id = ?", client.Id).Scan(&oldName).Error; err != nil {
		return err
	}
	if oldName == client.Name {
		return nil
	}
	if err := tx.Where("client_name = ?", client.Name).Delete(&model.ClientIP{}).Error; err != nil {
		return err
	}
	return tx.Model(&model.ClientIP{}).Where("client_name = ?", oldName).Update("client_name", client.Name).Error
}

func deleteClientIPHistory(tx *gorm.DB, clientIDs []uint) error {
	if len(clientIDs) == 0 {
		return nil
	}
	var names []string
	if err := tx.Model(model.Client{}).Where("id IN ?", clientIDs).Pluck("name", &names).Error; err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	return tx.Where("client_name IN ?", names).Delete(&model.ClientIP{}).Error
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
	if err := deleteClientIPHistory(req.Tx, ids); err != nil {
		return nil, err
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
	if err := deleteClientIPHistory(req.Tx, []uint{id}); err != nil {
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
	// The generic save payload is not an authority for subscription secrets.
	// New clients always receive a server-generated secret; edits preserve the
	// persisted secret and rotation uses the dedicated operation.
	secret, err := common.RandomUUID()
	if err != nil {
		return err
	}
	client.SubSecret = secret
	return nil
}

// EnsureSubSecret atomically backfills a missing legacy client secret. It is
// shared by every subscription lookup so there is one mutation authority for
// this persisted identity.
func EnsureSubSecret(tx *gorm.DB, client *model.Client) error {
	if tx == nil || client == nil || client.Id == 0 {
		return common.NewError("client subscription identity is unavailable")
	}
	if client.SubSecret != "" {
		return nil
	}
	secret, err := common.RandomUUID()
	if err != nil {
		return err
	}
	result := tx.Model(model.Client{}).
		Where("id = ? AND (sub_secret IS NULL OR sub_secret = '')", client.Id).
		Update("sub_secret", secret)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		client.SubSecret = secret
		return nil
	}
	return tx.Model(model.Client{}).Select("sub_secret").Where("id = ?", client.Id).First(client).Error
}

// RotateSubSecret is the sole explicit mutation path for an existing client's
// subscription identity.
func RotateSubSecret(tx *gorm.DB, clientID uint) (string, error) {
	if tx == nil || clientID == 0 {
		return "", common.NewError("invalid client id")
	}
	var client model.Client
	if err := tx.Model(model.Client{}).Select("id, name").Where("id = ?", clientID).First(&client).Error; err != nil {
		return "", err
	}
	newSecret, err := common.RandomUUID()
	if err != nil {
		return "", err
	}
	if err := tx.Model(model.Client{}).Where("id = ?", client.Id).Update("sub_secret", newSecret).Error; err != nil {
		return "", err
	}
	return client.Name, nil
}
