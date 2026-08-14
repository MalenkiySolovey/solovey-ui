package entitytls

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveidentity"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type SaveAction string

const (
	ActionNew  SaveAction = "new"
	ActionEdit SaveAction = "edit"
	ActionDel  SaveAction = "del"
)

var supportedSaveActions = []SaveAction{
	ActionNew,
	ActionEdit,
	ActionDel,
}

type CascadeHooks interface {
	UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error
	UpdateInboundOutJSONs(tx *gorm.DB, inboundIDs []uint, hostname string) error
	RestartInbounds(tx *gorm.DB, ids []uint) error
	RestartServices(tx *gorm.DB, ids []uint) error
}

type SaveRequest struct {
	Tx       *gorm.DB
	Action   string
	Data     json.RawMessage
	Hostname string
	Hooks    CascadeHooks
}

func GetAll(db *gorm.DB) ([]model.Tls, error) {
	tlsConfigs := []model.Tls{}
	err := db.Model(model.Tls{}).Where("id > 0").Order(entityorder.Clause).Scan(&tlsConfigs).Error
	if err != nil {
		return nil, err
	}
	return tlsConfigs, nil
}

func ParseAction(action string) (SaveAction, bool) {
	saveAction := SaveAction(action)
	for _, supported := range supportedSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}

func SupportedActionStrings() []string {
	actions := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}

func Save(req SaveRequest) error {
	action, ok := ParseAction(req.Action)
	if !ok {
		return common.NewErrorf("unknown action: %s", req.Action)
	}
	switch action {
	case ActionNew, ActionEdit:
		tls, err := saveConfig(req.Tx, string(action), req.Data)
		if err != nil {
			return err
		}
		if action == ActionEdit {
			return ApplyEditCascade(req.Tx, tls.Id, req.Hostname, req.Hooks)
		}
		return nil
	case ActionDel:
		return Delete(req.Tx, req.Data)
	default:
		return common.NewErrorf("unknown action: %s", req.Action)
	}
}

func saveConfig(tx *gorm.DB, action string, data json.RawMessage) (model.Tls, error) {
	var tls model.Tls
	if err := json.Unmarshal(data, &tls); err != nil {
		return tls, err
	}
	if err := saveidentity.Validate(tx, action, tls.Id, &model.Tls{}); err != nil {
		return tls, err
	}
	if err := entityidentity.ValidateName(tls.Name); err != nil {
		return tls, err
	}
	if err := validateJSONObject("TLS server", tls.Server); err != nil {
		return tls, err
	}
	if err := validateJSONObject("TLS client", tls.Client); err != nil {
		return tls, err
	}
	ApplySelfSignedPublicKeyPin(&tls)

	sortOrder, err := entityorder.ForSave(tx, &model.Tls{}, tls.Id)
	if err != nil {
		return tls, err
	}
	tls.SortOrder = sortOrder

	if err := tx.Save(&tls).Error; err != nil {
		return tls, err
	}
	return tls, nil
}

func validateJSONObject(label string, data json.RawMessage) error {
	var value map[string]interface{}
	if len(data) == 0 || json.Unmarshal(data, &value) != nil || value == nil {
		return common.NewError(label + " must be a JSON object")
	}
	return nil
}

// ValidateStored verifies TLS rows imported by migration or restore. The
// sentinel row is storage metadata and is intentionally excluded.
func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("TLS persistence is unavailable")
	}
	var rows []model.Tls
	if err := db.Where("id > 0").Order("id").Find(&rows).Error; err != nil {
		return fmt.Errorf("load stored TLS rows: %w", err)
	}
	for _, row := range rows {
		if err := entityidentity.ValidateName(row.Name); err != nil {
			return fmt.Errorf("stored TLS row %d: %w", row.Id, err)
		}
		if err := validateJSONObject("TLS server", row.Server); err != nil {
			return fmt.Errorf("stored TLS row %d: %w", row.Id, err)
		}
		if err := validateJSONObject("TLS client", row.Client); err != nil {
			return fmt.Errorf("stored TLS row %d: %w", row.Id, err)
		}
	}
	return nil
}

// EnsureSentinel owns the storage sentinel referenced by entities that do not
// use TLS. Migration, bootstrap and backup all call this one implementation.
func EnsureSentinel(db *gorm.DB) error {
	if db == nil {
		return common.NewError("TLS persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Tls{}) ||
		!db.Migrator().HasColumn(&model.Tls{}, "server") ||
		!db.Migrator().HasColumn(&model.Tls{}, "client") {
		return nil
	}
	return db.Exec(
		"INSERT OR IGNORE INTO tls(id, name, server, client) VALUES(0, ?, ?, ?)",
		"__none__", []byte("{}"), []byte("{}"),
	).Error
}

func ApplyEditCascade(tx *gorm.DB, tlsID uint, hostname string, hooks CascadeHooks) error {
	if err := RefreshInboundsUsingTLS(tx, tlsID, hostname, hooks); err != nil {
		return err
	}
	return RestartServicesUsingTLS(tx, tlsID, hooks)
}

func RefreshInboundsUsingTLS(tx *gorm.DB, tlsID uint, hostname string, hooks CascadeHooks) error {
	var inbounds []model.Inbound
	if err := tx.Model(model.Inbound{}).Preload("Tls").Where("tls_id = ?", tlsID).Find(&inbounds).Error; err != nil {
		return err
	}
	if len(inbounds) == 0 {
		return nil
	}
	if hooks == nil {
		return common.NewError("tls cascade hooks are not configured")
	}

	if err := hooks.UpdateLinksByInboundChange(tx, &inbounds, hostname, ""); err != nil {
		return err
	}
	inboundIDs := InboundIDsFromRows(inbounds)
	if err := hooks.UpdateInboundOutJSONs(tx, inboundIDs, hostname); err != nil {
		return common.NewError("unable to update out_json of inbounds: ", err.Error())
	}
	return hooks.RestartInbounds(tx, inboundIDs)
}

func RestartServicesUsingTLS(tx *gorm.DB, tlsID uint, hooks CascadeHooks) error {
	var serviceIDs []uint
	if err := tx.Model(model.Service{}).Where("tls_id = ?", tlsID).Pluck("id", &serviceIDs).Error; err != nil {
		return err
	}
	if len(serviceIDs) == 0 {
		return nil
	}
	if hooks == nil {
		return common.NewError("tls cascade hooks are not configured")
	}
	return hooks.RestartServices(tx, serviceIDs)
}

func InboundIDsFromRows(inbounds []model.Inbound) []uint {
	inboundIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		inboundIDs = append(inboundIDs, inbound.Id)
	}
	return inboundIDs
}

func Delete(tx *gorm.DB, data json.RawMessage) error {
	var id uint
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	if id == 0 {
		return common.NewError("tls id is required")
	}
	if err := EnsureNotInUse(tx, id); err != nil {
		return err
	}
	return tx.Where("id = ?", id).Delete(model.Tls{}).Error
}

func EnsureNotInUse(tx *gorm.DB, id uint) error {
	var inboundCount int64
	if err := tx.Model(model.Inbound{}).Where("tls_id = ?", id).Count(&inboundCount).Error; err != nil {
		return err
	}
	var serviceCount int64
	if err := tx.Model(model.Service{}).Where("tls_id = ?", id).Count(&serviceCount).Error; err != nil {
		return err
	}
	if inboundCount > 0 || serviceCount > 0 {
		return common.NewError("tls in use")
	}
	return nil
}
