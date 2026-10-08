package netentity

import (
	"encoding/json"
	"errors"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/sessionidentity"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"

	"gorm.io/gorm"
)

type InboundService struct {
	ClientHooks entityinbounds.ClientHooks
	Core        *coreruntime.Core
}

func (s *InboundService) Get(ids string) (*[]map[string]interface{}, error) {
	return entityinbounds.Get(dbsqlite.DB(), ids, s.userHooks())
}

func (s *InboundService) GetAll() (*[]map[string]interface{}, error) {
	return entityinbounds.GetAll(dbsqlite.DB(), s.userHooks())
}

func (s *InboundService) FromIds(ids []uint) ([]*model.Inbound, error) {
	return entityinbounds.FromIDs(dbsqlite.DB(), ids)
}

func (s *InboundService) Save(tx *gorm.DB, act string, data json.RawMessage, initUserIds string, hostname string) error {
	_, err := s.SaveWithCoreChange(tx, act, data, initUserIds, hostname)
	return err
}

func (s *InboundService) SaveWithCoreChange(tx *gorm.DB, act string, data json.RawMessage, initUserIds string, hostname string) (*singboxapply.Change, error) {
	return s.applyInboundSave(inboundSaveRequest{
		tx:          tx,
		action:      act,
		data:        data,
		initUserIDs: initUserIds,
		hostname:    hostname,
	})
}

func (s *InboundService) UpdateOutJsons(tx *gorm.DB, inboundIds []uint, hostname string) error {
	return entityinbounds.UpdateOutJSONs(tx, inboundIds, hostname)
}

func (s *InboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	return entityinbounds.GetAllConfig(db, s.userHooks())
}

func (s *InboundService) RestartInbounds(tx *gorm.DB, ids []uint) error {
	core := s.inboundCoreFromDB(tx)
	if core == nil || !core.IsRunning() {
		return nil
	}
	if tx == nil {
		return errors.New("inbound identity database unavailable")
	}
	// Rendering and credential binding share one read view, including callers
	// that supplied the autocommit DB rather than an existing transaction.
	return tx.Transaction(func(view *gorm.DB) error {
		return entityinbounds.Restart(view, ids, s.inboundCoreFromDB(view), s.userHooks())
	})
}

func (s *InboundService) RestartCurrentInbounds(ids []uint) error {
	return s.RestartInbounds(dbsqlite.DB(), ids)
}

func (s *InboundService) RemoveInboundsFromCore(tags []string) error {
	return entityinbounds.RemoveFromCore(tags, s.inboundCore())
}

func (s *InboundService) userHooks() entityinbounds.UserHooks {
	if s == nil {
		return inboundUserHooks{service: &InboundService{}}
	}
	return inboundUserHooks{service: s}
}

func (s *InboundService) clientHooks() entityinbounds.ClientHooks {
	if s == nil {
		return nil
	}
	return s.ClientHooks
}

func (s *InboundService) inboundCore() entityinbounds.Core {
	return s.inboundCoreFromDB(dbsqlite.DB())
}

func (s *InboundService) inboundCoreFromDB(db *gorm.DB) entityinbounds.Core {
	if s == nil {
		return nil
	}
	coreInstance := s.Core
	if coreInstance == nil {
		return nil
	}
	return inboundCoreAdapter{core: coreInstance, db: db}
}

type inboundUserHooks struct {
	service *InboundService
}

func (h inboundUserHooks) HasUser(inboundType string) bool {
	return h.service.hasUser(inboundType)
}

func (h inboundUserHooks) AddUsers(db *gorm.DB, inboundJSON []byte, inboundID uint, inboundType string) ([]byte, error) {
	return h.service.addUsers(db, inboundJSON, inboundID, inboundType)
}

func (h inboundUserHooks) ClientNamesByInboundIDs(db *gorm.DB, inboundIDs []uint) (map[uint][]string, error) {
	return entityclients.NamesByInboundIDs(db, inboundIDs)
}

type inboundCoreAdapter struct {
	core *coreruntime.Core
	db   *gorm.DB
}

func (a inboundCoreAdapter) IsRunning() bool {
	return a.core != nil && a.core.IsRunning()
}

func (a inboundCoreAdapter) RemoveInbound(tag string) error {
	return a.core.RemoveInbound(tag)
}

func (a inboundCoreAdapter) AddInbound(config []byte) error {
	bindings, err := sessionidentity.Capture(a.db, config)
	if err != nil {
		return err
	}
	return a.core.AddInboundWithBindings(config, bindings)
}

func (a inboundCoreAdapter) CloseInboundConnections(tag string) {
	if a.core == nil {
		return
	}
	a.core.CloseInboundConnections(tag)
}
