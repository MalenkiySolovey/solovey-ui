package netentity

import (
	"encoding/json"

	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

type tlsSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

func (s *TlsService) applyTLSSave(req tlsSaveRequest) error {
	if err := guardTLSFrontingLease(req.tx, req.action, req.data); err != nil {
		return err
	}
	return entitytls.Save(entitytls.SaveRequest{
		Tx:       req.tx,
		Action:   req.action,
		Data:     req.data,
		Hostname: req.hostname,
		Hooks:    s.tlsCascadeHooks(),
	})
}
