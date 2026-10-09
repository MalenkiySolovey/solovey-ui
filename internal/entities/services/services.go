package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/sagernet/sing-box/option"
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
				if _, fixed := srvData[k]; !fixed {
					srvData[k] = v
				}
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
		if !runtimeprojection.Included("services", srv.Type) {
			continue
		}
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
	if err := saveeligibility.Validate(tx, action, srv.Id, &model.Service{}, "services", srv.Type); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTypeTag(srv.Type, srv.Tag); err != nil {
		return nil, err
	}
	if srv.Type == "resolved" {
		var options option.ResolvedServiceOptions
		if err := options.UnmarshalJSONContext(context.Background(), srv.Options); err != nil {
			return nil, common.NewError("dns_resolved_service_schema_rejected: correct resolved listen options before saving")
		}
	}

	if srv.TlsId > 0 {
		if err := tx.Model(model.Tls{}).Where("id = ?", srv.TlsId).Find(&srv.Tls).Error; err != nil {
			return nil, err
		}
	}

	var old model.Service
	if srv.Id != 0 {
		if err := tx.Where("id = ?", srv.Id).Find(&old).Error; err != nil {
			return nil, err
		}
	}
	if old.Tag != "" && (old.Tag != srv.Tag || old.Type != srv.Type) {
		if err := rejectReferencedService(tx, old.Tag); err != nil {
			return nil, err
		}
	}
	if srv.Type == "resolved" {
		var count int64
		if err := tx.Model(&model.Service{}).Where("type = ? AND id <> ?", "resolved", srv.Id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, common.NewError("dns_resolved_service_duplicate: the official resolve1 service has one owner per core")
		}
	}

	var err error
	srv.SortOrder, err = entityorder.ForSave(tx, &model.Service{}, srv.Id)
	if err != nil {
		return nil, err
	}

	if err := tx.Save(&srv).Error; err != nil {
		return nil, err
	}
	change := &singboxapply.Change{ReloadIDs: []uint{srv.Id}}
	if old.Tag != "" && old.Tag != srv.Tag {
		change.RemoveTags = []string{old.Tag}
	}
	if old.Type == "resolved" || srv.Type == "resolved" {
		change.NeedsRestart = true
		change.RestartReason = "dns resolved service lifecycle changed"
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
	if err := rejectReferencedService(tx, tag); err != nil {
		return nil, err
	}
	var old model.Service
	if err := tx.Where("tag = ?", tag).Find(&old).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("tag = ?", tag).Delete(model.Service{}).Error; err != nil {
		return nil, err
	}
	return &singboxapply.Change{RemoveTags: []string{tag}, NeedsRestart: old.Type == "resolved", RestartReason: "dns resolved service lifecycle changed"}, nil
}

func rejectReferencedService(tx *gorm.DB, tag string) error {
	refs, err := tagrefs.Service(tx, tag)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return tagrefs.FormatError("service", tag, refs)
	}
	return nil
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
	if _, err := runtimeprojection.ValidateReferences(tx, nil); err != nil {
		return err
	}
	var rows []*model.Service
	if err := tx.Model(model.Service{}).Preload("Tls").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	type prepared struct {
		tag     string
		configs []json.RawMessage
	}
	ready := []prepared{}
	// Complete capability/serialization preflight for the whole batch before
	// the first removal. Unavailable historical rows do not mutate live state.
	for _, row := range rows {
		if !runtimeprojection.Included("services", row.Type) {
			continue
		}

		config, err := row.MarshalJSON()
		if err != nil {
			return err
		}
		configs := []json.RawMessage{config}
		ready = append(ready, prepared{row.Tag, configs})
	}

	for _, row := range ready {
		if err := core.RemoveService(row.tag); err != nil && err != os.ErrInvalid {
			return err
		}

		for _, config := range row.configs {

			if err := core.AddService(config); err != nil {
				return err
			}
		}
	}
	return nil
}
