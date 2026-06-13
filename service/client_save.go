package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type clientSaveAction string

const (
	clientSaveActionNew      clientSaveAction = "new"
	clientSaveActionEdit     clientSaveAction = "edit"
	clientSaveActionAddBulk  clientSaveAction = "addbulk"
	clientSaveActionEditBulk clientSaveAction = "editbulk"
	clientSaveActionDelBulk  clientSaveAction = "delbulk"
	clientSaveActionDel      clientSaveAction = "del"
)

var supportedClientSaveActions = []clientSaveAction{
	clientSaveActionNew,
	clientSaveActionEdit,
	clientSaveActionAddBulk,
	clientSaveActionEditBulk,
	clientSaveActionDelBulk,
	clientSaveActionDel,
}

type clientSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

type clientSaveHandler func(*ClientService, clientSaveRequest) ([]uint, error)

var clientSaveHandlers = map[clientSaveAction]clientSaveHandler{
	clientSaveActionNew:      saveNewClient,
	clientSaveActionEdit:     saveEditedClient,
	clientSaveActionAddBulk:  saveAddedBulkClients,
	clientSaveActionEditBulk: saveEditedBulkClients,
	clientSaveActionDelBulk:  saveDeletedBulkClients,
	clientSaveActionDel:      saveDeletedClient,
}

func (s *ClientService) applyClientSave(req clientSaveRequest) ([]uint, error) {
	action, ok := parseClientSaveAction(req.action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.action)
	}
	return clientSaveHandlers[action](s, req)
}

func parseClientSaveAction(action string) (clientSaveAction, bool) {
	saveAction := clientSaveAction(action)
	for _, supported := range supportedClientSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}

func supportedClientSaveActionStrings() []string {
	actions := make([]string, 0, len(supportedClientSaveActions))
	for _, action := range supportedClientSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}

func saveNewClient(s *ClientService, req clientSaveRequest) ([]uint, error) {
	return saveSingleClient(s, req, false)
}

func saveEditedClient(s *ClientService, req clientSaveRequest) ([]uint, error) {
	return saveSingleClient(s, req, true)
}

func saveSingleClient(s *ClientService, req clientSaveRequest, editing bool) ([]uint, error) {
	var client model.Client
	if err := json.Unmarshal(req.data, &client); err != nil {
		return nil, err
	}
	if err := s.prepareClientSubSecret(req.tx, &client, editing); err != nil {
		return nil, err
	}
	if err := s.updateLinksWithFixedInbounds(req.tx, []*model.Client{&client}, req.hostname); err != nil {
		return nil, err
	}
	sortOrder, err := sortOrderForSave(req.tx, &model.Client{}, client.Id)
	if err != nil {
		return nil, err
	}
	client.SortOrder = sortOrder

	var inboundIds []uint
	if editing {
		changedInboundIds, err := s.findInboundsChanges(req.tx, &client, false)
		if err != nil {
			return nil, err
		}
		inboundIds = changedInboundIds
	} else if err := json.Unmarshal(client.Inbounds, &inboundIds); err != nil {
		return nil, err
	}

	if err := req.tx.Save(&client).Error; err != nil {
		return nil, err
	}
	return inboundIds, nil
}

func saveAddedBulkClients(s *ClientService, req clientSaveRequest) ([]uint, error) {
	var clients []*model.Client
	if err := json.Unmarshal(req.data, &clients); err != nil {
		return nil, err
	}
	var inboundIds []uint
	if len(clients) == 0 {
		return inboundIds, nil
	}

	// addbulk clients all share the same inbound set (the frontend forces an
	// identical Inbounds array), so clients[0] is representative here.
	if err := json.Unmarshal(clients[0].Inbounds, &inboundIds); err != nil {
		return nil, err
	}
	for _, client := range clients {
		if err := s.prepareClientSubSecret(req.tx, client, false); err != nil {
			return nil, err
		}
	}
	nextOrder, err := nextSortOrder(req.tx, &model.Client{})
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		client.SortOrder = nextOrder
		nextOrder++
	}
	if err := s.updateLinksWithFixedInbounds(req.tx, clients, req.hostname); err != nil {
		return nil, err
	}
	if err := database.SaveInBatchesSafe(req.tx, clients); err != nil {
		return nil, err
	}
	return inboundIds, nil
}

func saveEditedBulkClients(s *ClientService, req clientSaveRequest) ([]uint, error) {
	var clients []*model.Client
	if err := json.Unmarshal(req.data, &clients); err != nil {
		return nil, err
	}

	var inboundIds []uint
	for _, client := range clients {
		if err := s.prepareClientSubSecret(req.tx, client, true); err != nil {
			return nil, err
		}
		changedInboundIds, err := s.findInboundsChanges(req.tx, client, true)
		if err != nil {
			return nil, err
		}
		if len(changedInboundIds) > 0 {
			inboundIds = common.UnionUintArray(inboundIds, changedInboundIds)
		}
		sortOrder, err := sortOrderForSave(req.tx, &model.Client{}, client.Id)
		if err != nil {
			return nil, err
		}
		client.SortOrder = sortOrder
	}
	if len(inboundIds) > 0 {
		if err := s.updateLinksWithFixedInbounds(req.tx, clients, req.hostname); err != nil {
			return nil, err
		}
	}
	if err := database.SaveInBatchesSafe(req.tx, clients); err != nil {
		return nil, err
	}
	return inboundIds, nil
}

func saveDeletedBulkClients(s *ClientService, req clientSaveRequest) ([]uint, error) {
	var ids []uint
	if err := json.Unmarshal(req.data, &ids); err != nil {
		return nil, err
	}

	var inboundIds []uint
	for _, id := range ids {
		clientInbounds, err := clientInboundsByID(req.tx, id)
		if err != nil {
			return nil, err
		}
		inboundIds = common.UnionUintArray(inboundIds, clientInbounds)
	}
	if err := req.tx.Where("id in ?", ids).Delete(model.Client{}).Error; err != nil {
		return nil, err
	}
	return inboundIds, nil
}

func saveDeletedClient(s *ClientService, req clientSaveRequest) ([]uint, error) {
	var id uint
	if err := json.Unmarshal(req.data, &id); err != nil {
		return nil, err
	}

	inboundIds, err := clientInboundsByID(req.tx, id)
	if err != nil {
		return nil, err
	}
	if err := req.tx.Where("id = ?", id).Delete(model.Client{}).Error; err != nil {
		return nil, err
	}
	return inboundIds, nil
}

func clientInboundsByID(tx *gorm.DB, id uint) ([]uint, error) {
	var client model.Client
	if err := tx.Where("id = ?", id).First(&client).Error; err != nil {
		return nil, err
	}
	var inboundIds []uint
	if err := json.Unmarshal(client.Inbounds, &inboundIds); err != nil {
		return nil, err
	}
	return inboundIds, nil
}
