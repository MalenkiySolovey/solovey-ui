package netentity

import (
	"encoding/json"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
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
	return entityinbounds.Restart(tx, ids, s.inboundCore(), s.userHooks())
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
	if s == nil {
		return nil
	}
	coreInstance := s.Core
	if coreInstance == nil {
		return nil
	}
	return inboundCoreAdapter{core: coreInstance}
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
}

func (a inboundCoreAdapter) IsRunning() bool {
	return a.core != nil && a.core.IsRunning()
}

func (a inboundCoreAdapter) RemoveInbound(tag string) error {
	return a.core.RemoveInbound(tag)
}

func (a inboundCoreAdapter) AddInbound(config []byte) error {
	return a.core.AddInbound(config)
}

func (a inboundCoreAdapter) CloseInboundConnections(tag string) {
	if a.core == nil {
		return
	}
	if instance := a.core.GetInstance(); instance != nil {
		if tracker := instance.ConnTracker(); tracker != nil {
			tracker.CloseConnByInbound(tag)
		}
	}
}
