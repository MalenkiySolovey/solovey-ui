package netentity

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

const (
	tlsSaveActionNew  = entitytls.ActionNew
	tlsSaveActionEdit = entitytls.ActionEdit
	tlsSaveActionDel  = entitytls.ActionDel
)

type tlsSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

type tlsSaveHandler func(*TlsService, tlsSaveRequest) error

var tlsSaveHandlers = map[entitytls.SaveAction]tlsSaveHandler{
	tlsSaveActionNew:  saveNewTLS,
	tlsSaveActionEdit: saveEditedTLS,
	tlsSaveActionDel:  saveDeletedTLS,
}

func (s *TlsService) applyTLSSave(req tlsSaveRequest) error {
	action, ok := parseTLSSaveAction(req.action)
	if !ok {
		return nil
	}
	return tlsSaveHandlers[action](s, req)
}

func parseTLSSaveAction(action string) (entitytls.SaveAction, bool) {
	return entitytls.ParseAction(action)
}

func saveNewTLS(s *TlsService, req tlsSaveRequest) error {
	_, err := saveTLSConfig(req.tx, req.data)
	return err
}

func saveEditedTLS(s *TlsService, req tlsSaveRequest) error {
	tls, err := saveTLSConfig(req.tx, req.data)
	if err != nil {
		return err
	}
	return s.applyTLSEditCascade(req.tx, tls.Id, req.hostname)
}

func saveTLSConfig(tx *gorm.DB, data json.RawMessage) (model.Tls, error) {
	return entitytls.SaveConfig(tx, data)
}

func (s *TlsService) applyTLSEditCascade(tx *gorm.DB, tlsID uint, hostname string) error {
	return entitytls.ApplyEditCascade(tx, tlsID, hostname, s.tlsCascadeHooks())
}

func saveDeletedTLS(s *TlsService, req tlsSaveRequest) error {
	return entitytls.Delete(req.tx, req.data)
}
