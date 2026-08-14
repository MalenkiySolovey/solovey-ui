package netentity

import (
	"encoding/json"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"

	"gorm.io/gorm"
)

type inboundSaveRequest struct {
	tx          *gorm.DB
	action      string
	data        json.RawMessage
	initUserIDs string
	hostname    string
}

func (s *InboundService) applyInboundSave(req inboundSaveRequest) (*singboxapply.Change, error) {
	if err := guardInboundFrontingLease(req.tx, req.action, req.data); err != nil {
		return nil, err
	}
	return entityinbounds.Save(entityinbounds.SaveRequest{
		Tx:          req.tx,
		Action:      req.action,
		Data:        req.data,
		InitUserIDs: req.initUserIDs,
		Hostname:    req.hostname,
		ClientHooks: s.clientHooks(),
	})
}
