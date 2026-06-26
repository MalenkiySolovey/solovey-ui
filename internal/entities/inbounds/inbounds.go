package entityinbounds

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
	"strings"
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

type ClientHooks interface {
	UpdateClientsOnInboundAdd(tx *gorm.DB, initIDs string, inboundID uint, hostname string) error
	UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error
	UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error
}
type UserHooks interface {
	HasUser(inboundType string) bool
	AddUsers(db *gorm.DB, inboundJSON []byte, inboundID uint, inboundType string) ([]byte, error)
	ClientNamesByInboundIDs(db *gorm.DB, inboundIDs []uint) (map[uint][]string, error)
}
type Core interface {
	IsRunning() bool
	RemoveInbound(tag string) error
	AddInbound(config []byte) error
	CloseInboundConnections(tag string)
}
type SaveRequest struct {
	Tx          *gorm.DB
	Action      string
	Data        json.RawMessage
	InitUserIDs string
	Hostname    string
	ClientHooks ClientHooks
}
type listItem struct {
	id           uint
	data         map[string]interface{}
	includeUsers bool
}

func Get(db *gorm.DB, ids string, hooks UserHooks) (*[]map[string]interface{}, error) {
	if ids == "" {
		return GetAll(db, hooks)
	}
	return GetByIDs(db, ids)
}
func GetByIDs(db *gorm.DB, ids string) (*[]map[string]interface{}, error) {
	var inbound []model.Inbound
	var result []map[string]interface{}
	err := db.Model(model.Inbound{}).Where("id in ?", strings.Split(ids, ",")).Order(entityorder.Clause).Scan(&inbound).Error
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
func GetAll(db *gorm.DB, hooks UserHooks) (*[]map[string]interface{}, error) {
	inbounds := []model.Inbound{}
	err := db.Model(model.Inbound{}).Order(entityorder.Clause).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	items := make([]listItem, 0, len(inbounds))
	userInboundIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		inbData, includeUsers, err := listDataForInbound(inbound, hooks)
		if err != nil {
			return nil, err
		}
		if includeUsers {
			userInboundIDs = append(userInboundIDs, inbound.Id)
		}
		items = append(items, listItem{
			id:           inbound.Id,
			data:         inbData,
			includeUsers: includeUsers,
		})
	}
	usersByInbound, err := clientNamesByInboundIDs(db, userInboundIDs, hooks)
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
func listDataForInbound(inbound model.Inbound, hooks UserHooks) (map[string]interface{}, bool, error) {
	var shadowtlsVersion uint
	ssManaged := false
	inbData := map[string]interface{}{
		"id":        inbound.Id,
		"sortOrder": inbound.SortOrder,
		"type":      inbound.Type,
		"tag":       inbound.Tag,
		"tls_id":    inbound.TlsId,
	}
	if inbound.Options != nil {
		var restFields map[string]json.RawMessage
		if err := json.Unmarshal(inbound.Options, &restFields); err != nil {
			return nil, false, err
		}
		inbData["listen"] = restFields["listen"]
		inbData["listen_port"] = restFields["listen_port"]
		if inbound.Type == "shadowtls" {
			_ = json.Unmarshal(restFields["version"], &shadowtlsVersion)
		}
		if inbound.Type == "shadowsocks" {
			_ = json.Unmarshal(restFields["managed"], &ssManaged)
		}
	}
	includeUsers := hooks != nil &&
		hooks.HasUser(inbound.Type) &&
		!(inbound.Type == "shadowtls" && shadowtlsVersion < 3) &&
		!(inbound.Type == "shadowsocks" && ssManaged)
	return inbData, includeUsers, nil
}
func clientNamesByInboundIDs(db *gorm.DB, inboundIDs []uint, hooks UserHooks) (map[uint][]string, error) {
	usersByInbound := make(map[uint][]string, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return usersByInbound, nil
	}
	if hooks == nil {
		return nil, common.NewError("inbound user hooks are not configured")
	}
	return hooks.ClientNamesByInboundIDs(db, inboundIDs)
}
func FromIDs(db *gorm.DB, ids []uint) ([]*model.Inbound, error) {
	inbounds := []*model.Inbound{}
	err := db.Model(model.Inbound{}).Where("id in ?", ids).Order(entityorder.Clause).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}
