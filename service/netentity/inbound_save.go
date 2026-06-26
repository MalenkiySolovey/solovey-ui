package netentity

import (
	"encoding/json"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const (
	inboundSaveActionNew  = entityinbounds.ActionNew
	inboundSaveActionEdit = entityinbounds.ActionEdit
	inboundSaveActionDel  = entityinbounds.ActionDel
)

type inboundSaveRequest struct {
	tx          *gorm.DB
	action      string
	data        json.RawMessage
	initUserIDs string
	hostname    string
}

type inboundSaveHandler func(*InboundService, inboundSaveRequest) (*singboxapply.Change, error)

var inboundSaveHandlers = map[entityinbounds.SaveAction]inboundSaveHandler{
	inboundSaveActionNew:  saveNewInbound,
	inboundSaveActionEdit: saveEditedInbound,
	inboundSaveActionDel:  saveDeletedInbound,
}

func (s *InboundService) applyInboundSave(req inboundSaveRequest) (*singboxapply.Change, error) {
	action, ok := parseInboundSaveAction(req.action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.action)
	}
	return inboundSaveHandlers[action](s, req)
}

func parseInboundSaveAction(action string) (entityinbounds.SaveAction, bool) {
	return entityinbounds.ParseAction(action)
}

func saveNewInbound(s *InboundService, req inboundSaveRequest) (*singboxapply.Change, error) {
	return entityinbounds.Save(entityinbounds.SaveRequest{
		Tx:          req.tx,
		Action:      req.action,
		Data:        req.data,
		InitUserIDs: req.initUserIDs,
		Hostname:    req.hostname,
		ClientHooks: s.clientHooks(),
	})
}

func saveEditedInbound(s *InboundService, req inboundSaveRequest) (*singboxapply.Change, error) {
	return entityinbounds.Save(entityinbounds.SaveRequest{
		Tx:          req.tx,
		Action:      req.action,
		Data:        req.data,
		InitUserIDs: req.initUserIDs,
		Hostname:    req.hostname,
		ClientHooks: s.clientHooks(),
	})
}

func saveDeletedInbound(s *InboundService, req inboundSaveRequest) (*singboxapply.Change, error) {
	return entityinbounds.Save(entityinbounds.SaveRequest{
		Tx:          req.tx,
		Action:      req.action,
		Data:        req.data,
		InitUserIDs: req.initUserIDs,
		Hostname:    req.hostname,
		ClientHooks: s.clientHooks(),
	})
}
