package services

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveidentity"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type Core interface {
	IsRunning() bool
	RemoveService(tag string) error
	AddService(config []byte) error
}

func GetAll(db *gorm.DB) (*[]map[string]interface{}, error) {
	services := []model.Service{}
	err := db.Model(model.Service{}).Order(entityorder.Clause).Scan(&services).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, srv := range services {
		srvData := map[string]interface{}{
			"id":        srv.Id,
			"sortOrder": srv.SortOrder,
			"type":      srv.Type,
			"tag":       srv.Tag,
			"tls_id":    srv.TlsId,
		}
		if srv.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(srv.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				srvData[k] = v
			}
		}

		data = append(data, srvData)
	}
	return &data, nil
}

func GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var servicesJSON []json.RawMessage
	var rows []*model.Service
	err := db.Model(model.Service{}).Preload("Tls").Order(entityorder.Clause).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, srv := range rows {
		srvJSON, err := srv.MarshalJSON()
		if err != nil {
			return nil, err
		}
		servicesJSON = append(servicesJSON, srvJSON)
	}
	return servicesJSON, nil
}

func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("service persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Service{}) {
		return nil
	}
	var rows []model.Service
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := jsonvalue.OptionalObject("service options", row.Options); err != nil {
			return fmt.Errorf("stored service row %d: %w", row.Id, err)
		}
	}
	return nil
}

func Save(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	switch act {
	case "new", "edit":
		return saveUpsert(tx, act, data)
	case "del":
		return saveDelete(tx, data)
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
}

func saveUpsert(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	var srv model.Service
	if err := srv.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	if err := saveidentity.Validate(tx, action, srv.Id, &model.Service{}); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTypeTag(srv.Type, srv.Tag); err != nil {
		return nil, err
	}

	if srv.TlsId > 0 {
		if err := tx.Model(model.Tls{}).Where("id = ?", srv.TlsId).Find(&srv.Tls).Error; err != nil {
			return nil, err
		}
	}

	oldTag, err := tagByID(tx, srv.Id)
	if err != nil {
		return nil, err
	}

	srv.SortOrder, err = entityorder.ForSave(tx, &model.Service{}, srv.Id)
	if err != nil {
		return nil, err
	}

	if err := tx.Save(&srv).Error; err != nil {
		return nil, err
	}
	change := &singboxapply.Change{ReloadIDs: []uint{srv.Id}}
	if oldTag != "" && oldTag != srv.Tag {
		change.RemoveTags = []string{oldTag}
	}
	return change, nil
}

func saveDelete(tx *gorm.DB, data json.RawMessage) (*singboxapply.Change, error) {
	var tag string
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTag(tag); err != nil {
		return nil, err
	}
	if err := tx.Where("tag = ?", tag).Delete(model.Service{}).Error; err != nil {
		return nil, err
	}
	return &singboxapply.Change{RemoveTags: []string{tag}}, nil
}

func tagByID(tx *gorm.DB, id uint) (string, error) {
	if id == 0 {
		return "", nil
	}
	var tag string
	err := tx.Model(model.Service{}).Select("tag").Where("id = ?", id).Find(&tag).Error
	return tag, err
}

func RemoveFromCore(tags []string, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	for _, tag := range tags {
		if err := core.RemoveService(tag); err != nil && err != os.ErrInvalid {
			return err
		}
	}
	return nil
}

func Restart(tx *gorm.DB, ids []uint, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	var rows []*model.Service
	err := tx.Model(model.Service{}).Preload("Tls").Where("id in ?", ids).Find(&rows).Error
	if err != nil {
		return err
	}
	for _, srv := range rows {
		err = core.RemoveService(srv.Tag)
		if err != nil && err != os.ErrInvalid {
			return err
		}
		srvConfig, err := srv.MarshalJSON()
		if err != nil {
			return err
		}
		err = core.AddService(srvConfig)
		if err != nil {
			return err
		}
	}
	return nil
}
