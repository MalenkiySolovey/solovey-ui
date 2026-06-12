package service

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util"

	"gorm.io/gorm"
)

type InboundService struct {
	ClientService
	Runtime *Runtime
}

func (s *InboundService) runtime() *Runtime {
	if s != nil {
		return runtimeOrDefault(s.Runtime)
	}
	return DefaultRuntime()
}

type inboundListItem struct {
	id           uint
	data         map[string]interface{}
	includeUsers bool
}

func (s *InboundService) Get(ids string) (*[]map[string]interface{}, error) {
	if ids == "" {
		return s.GetAll()
	}
	return s.getById(ids)
}

func (s *InboundService) getById(ids string) (*[]map[string]interface{}, error) {
	var inbound []model.Inbound
	var result []map[string]interface{}
	db := database.GetDB()
	err := db.Model(model.Inbound{}).Where("id in ?", strings.Split(ids, ",")).Scan(&inbound).Error
	if err != nil {
		return nil, err
	}
	for _, inb := range inbound {
		inbData, err := inb.MarshalFull()
		if err != nil {
			return nil, err
		}
		result = append(result, *inbData)
	}
	return &result, nil
}

func (s *InboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	inbounds := []model.Inbound{}
	err := db.Model(model.Inbound{}).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	items := make([]inboundListItem, 0, len(inbounds))
	userInboundIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		var shadowtls_version uint
		ss_managed := false
		inbData := map[string]interface{}{
			"id":     inbound.Id,
			"type":   inbound.Type,
			"tag":    inbound.Tag,
			"tls_id": inbound.TlsId,
		}
		if inbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(inbound.Options, &restFields); err != nil {
				return nil, err
			}
			inbData["listen"] = restFields["listen"]
			inbData["listen_port"] = restFields["listen_port"]
			if inbound.Type == "shadowtls" {
				_ = json.Unmarshal(restFields["version"], &shadowtls_version)
			}
			if inbound.Type == "shadowsocks" {
				_ = json.Unmarshal(restFields["managed"], &ss_managed)
			}
		}
		includeUsers := s.hasUser(inbound.Type) &&
			!(inbound.Type == "shadowtls" && shadowtls_version < 3) &&
			!(inbound.Type == "shadowsocks" && ss_managed)
		if includeUsers {
			userInboundIDs = append(userInboundIDs, inbound.Id)
		}

		items = append(items, inboundListItem{
			id:           inbound.Id,
			data:         inbData,
			includeUsers: includeUsers,
		})
	}
	usersByInbound, err := clientNamesByInboundIDs(db, userInboundIDs)
	if err != nil {
		return nil, err
	}
	data := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.includeUsers {
			item.data["users"] = usersByInbound[item.id]
		}
		data = append(data, item.data)
	}
	return &data, nil
}

func (s *InboundService) FromIds(ids []uint) ([]*model.Inbound, error) {
	db := database.GetDB()
	inbounds := []*model.Inbound{}
	err := db.Model(model.Inbound{}).Where("id in ?", ids).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) Save(tx *gorm.DB, act string, data json.RawMessage, initUserIds string, hostname string) error {
	return s.applyInboundSave(inboundSaveRequest{
		tx:          tx,
		action:      act,
		data:        data,
		initUserIDs: initUserIds,
		hostname:    hostname,
	})
}

func (s *InboundService) UpdateOutJsons(tx *gorm.DB, inboundIds []uint, hostname string) error {
	var inbounds []model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIds).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = util.FillOutJson(&inbound, hostname)
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("tag = ?", inbound.Tag).Update("out_json", inbound.OutJson).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *InboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var inboundsJson []json.RawMessage
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("Tls").Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		inboundJson, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		inboundJson, err = s.addUsers(db, inboundJson, inbound.Id, inbound.Type)
		if err != nil {
			return nil, err
		}
		inboundsJson = append(inboundsJson, inboundJson)
	}
	return inboundsJson, nil
}

func (s *InboundService) RestartInbounds(tx *gorm.DB, ids []uint) error {
	coreInstance := s.runtime().Core()
	if coreInstance == nil || !coreInstance.IsRunning() {
		return nil
	}
	var inbounds []*model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = coreInstance.RemoveInbound(inbound.Tag)
		if err != nil && err != os.ErrInvalid {
			return err
		}
		// Close all existing connections. The core may have been stopped
		// concurrently (cron / user restart), so guard against a nil instance.
		if instance := coreInstance.GetInstance(); instance != nil {
			if tracker := instance.ConnTracker(); tracker != nil {
				tracker.CloseConnByInbound(inbound.Tag)
			}
		}

		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return err
		}
		inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
		if err != nil {
			return err
		}
		err = coreInstance.AddInbound(inboundConfig)
		if err != nil {
			return err
		}
	}
	return nil
}
