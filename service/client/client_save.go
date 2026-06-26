package client

import (
	"encoding/json"

	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const (
	clientSaveActionNew      = entityclients.ActionNew
	clientSaveActionEdit     = entityclients.ActionEdit
	clientSaveActionAddBulk  = entityclients.ActionAddBulk
	clientSaveActionEditBulk = entityclients.ActionEditBulk
	clientSaveActionDelBulk  = entityclients.ActionDelBulk
	clientSaveActionDel      = entityclients.ActionDel
)

type clientSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

type clientSaveHandler func(*Service, clientSaveRequest) ([]uint, error)

var clientSaveHandlers = map[entityclients.SaveAction]clientSaveHandler{
	clientSaveActionNew:      saveNewClient,
	clientSaveActionEdit:     saveEditedClient,
	clientSaveActionAddBulk:  saveAddedBulkClients,
	clientSaveActionEditBulk: saveEditedBulkClients,
	clientSaveActionDelBulk:  saveDeletedBulkClients,
	clientSaveActionDel:      saveDeletedClient,
}

func (s *Service) applyClientSave(req clientSaveRequest) ([]uint, error) {
	action, ok := parseClientSaveAction(req.action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.action)
	}
	return clientSaveHandlers[action](s, req)
}

func parseClientSaveAction(action string) (entityclients.SaveAction, bool) {
	return entityclients.ParseAction(action)
}

func saveNewClient(s *Service, req clientSaveRequest) ([]uint, error) {
	return saveSingleClient(s, req, false)
}

func saveEditedClient(s *Service, req clientSaveRequest) ([]uint, error) {
	return saveSingleClient(s, req, true)
}

func saveSingleClient(s *Service, req clientSaveRequest, editing bool) ([]uint, error) {
	action := string(clientSaveActionNew)
	if editing {
		action = string(clientSaveActionEdit)
	}
	return entityclients.Save(entityclients.SaveRequest{
		Tx:       req.tx,
		Action:   action,
		Data:     req.data,
		Hostname: req.hostname,
	})
}

func saveAddedBulkClients(s *Service, req clientSaveRequest) ([]uint, error) {
	return entityclients.Save(entityclients.SaveRequest{
		Tx:       req.tx,
		Action:   string(clientSaveActionAddBulk),
		Data:     req.data,
		Hostname: req.hostname,
	})
}

func saveEditedBulkClients(s *Service, req clientSaveRequest) ([]uint, error) {
	return entityclients.Save(entityclients.SaveRequest{
		Tx:       req.tx,
		Action:   string(clientSaveActionEditBulk),
		Data:     req.data,
		Hostname: req.hostname,
	})
}

func saveDeletedBulkClients(s *Service, req clientSaveRequest) ([]uint, error) {
	return entityclients.Save(entityclients.SaveRequest{
		Tx:       req.tx,
		Action:   string(clientSaveActionDelBulk),
		Data:     req.data,
		Hostname: req.hostname,
	})
}

func saveDeletedClient(s *Service, req clientSaveRequest) ([]uint, error) {
	return entityclients.Save(entityclients.SaveRequest{
		Tx:       req.tx,
		Action:   string(clientSaveActionDel),
		Data:     req.data,
		Hostname: req.hostname,
	})
}
