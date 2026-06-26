package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type dbReorderTarget struct {
	entityorder.DBTarget
	reload      []string
	coreRestart bool
}

func (s *ConfigService) Reorder(obj string, data json.RawMessage, loginUser string) ([]string, error) {
	obj = normalizeReorderObject(obj)
	plan := newConfigSavePlan(primaryReorderObject(obj))

	db := dbsqlite.DB()
	tx := db.Begin()
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	if target, ok := s.dbReorderTarget(obj); ok {
		if err := s.reorderDBTarget(tx, target, data); err != nil {
			return nil, err
		}
		plan.IncludeObjects(target.reload...)
		if target.coreRestart {
			plan.RequireCoreRestart()
		}
	} else if err := s.reorderConfigTarget(tx, obj, data, &plan); err != nil {
		return nil, err
	}

	if err := s.recordConfigChange(tx, loginUser, obj, "reorder", data); err != nil {
		return nil, err
	}
	s.setLastUpdate(time.Now().Unix())

	if err := tx.Commit().Error; err != nil {
		return plan.Objects(), err
	}
	committed = true
	s.applyConfigSaveEffects(plan, loginUser, false, false)
	return plan.Objects(), nil
}

func normalizeReorderObject(obj string) string {
	switch strings.TrimSpace(obj) {
	case "users":
		return "admins"
	case "dns_servers", "dnsServers":
		return "dnsServers"
	case "rulesets", "ruleSets", "rule_set":
		return "ruleSets"
	default:
		return strings.TrimSpace(obj)
	}
}

func primaryReorderObject(obj string) string {
	switch obj {
	case "dnsServers", "ruleSets":
		return "config"
	default:
		return obj
	}
}

func (s *ConfigService) dbReorderTarget(obj string) (dbReorderTarget, bool) {
	switch obj {
	case "inbounds":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.Inbound{}}, reload: []string{"inbounds"}}, true
	case "clients":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.Client{}}, reload: []string{"clients", "inbounds"}}, true
	case "outbounds":
		return dbReorderTarget{
			DBTarget: entityorder.DBTarget{
				ModelValue: &model.Outbound{},
				Before:     s.preserveImplicitRouteFinal,
			},
			reload: []string{"outbounds", "config"},
		}, true
	case "endpoints":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.Endpoint{}}, reload: []string{"endpoints"}}, true
	case "services":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.Service{}}, reload: []string{"services"}}, true
	case "tls":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.Tls{}, Where: "id > 0"}, reload: []string{"tls"}}, true
	case "admins":
		return dbReorderTarget{DBTarget: entityorder.DBTarget{ModelValue: &model.User{}}, reload: []string{"users"}}, true
	default:
		return dbReorderTarget{}, false
	}
}

func (s *ConfigService) reorderDBTarget(tx *gorm.DB, target dbReorderTarget, data json.RawMessage) error {
	return entityorder.ReorderDBTarget(tx, target.DBTarget, data)
}

func (s *ConfigService) reorderConfigTarget(tx *gorm.DB, obj string, data json.RawMessage, plan *configSavePlan) error {
	tags, err := parseReorderTags(data)
	if err != nil {
		return err
	}
	doc, err := s.baseConfigDocument(tx)
	if err != nil {
		return err
	}

	switch obj {
	case "dnsServers":
		if err := preserveImplicitDNSFinal(doc); err != nil {
			return err
		}
		if err := reorderTaggedConfigArray(doc, "dns", "servers", tags); err != nil {
			return err
		}
	case "ruleSets":
		if err := reorderTaggedConfigArray(doc, "route", "rule_set", tags); err != nil {
			return err
		}
	default:
		return common.NewError("unknown reorder object:", obj)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := NewSingBoxBaseConfigStore(&s.SettingService).Save(tx, raw); err != nil {
		return err
	}
	return nil
}

func parseReorderTags(data json.RawMessage) ([]string, error) {
	var tags []string
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return nil, common.NewError("reorder tag can not be empty")
		}
	}
	return tags, nil
}

func (s *ConfigService) baseConfigDocument(tx *gorm.DB) (map[string]any, error) {
	config, err := baseConfigFromTx(tx)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func baseConfigFromTx(tx *gorm.DB) ([]byte, error) {
	var configValue string
	result := tx.Model(model.Setting{}).Select("value").Where("key = ?", "config").Scan(&configValue)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 || strings.TrimSpace(configValue) == "" {
		configValue = defaultSingBoxBaseConfig
	}
	return []byte(configValue), nil
}

func reorderTaggedConfigArray(doc map[string]any, section string, key string, requested []string) error {
	sectionObj := ensureConfigObject(doc, section)
	rawItems, _ := sectionObj[key].([]any)
	if rawItems == nil {
		rawItems = []any{}
	}

	current := make([]string, 0, len(rawItems))
	byTag := make(map[string]any, len(rawItems))
	for _, item := range rawItems {
		itemObj, ok := item.(map[string]any)
		if !ok {
			return common.NewErrorf("config.%s.%s must contain JSON objects", section, key)
		}
		tag, _ := itemObj["tag"].(string)
		if strings.TrimSpace(tag) == "" {
			return common.NewErrorf("config.%s.%s has an item without tag", section, key)
		}
		if _, exists := byTag[tag]; exists {
			return common.NewErrorf("config.%s.%s has duplicate tag %q", section, key, tag)
		}
		current = append(current, tag)
		byTag[tag] = item
	}
	if err := validateReorderTags(current, requested); err != nil {
		return err
	}

	reordered := make([]any, 0, len(requested))
	for _, tag := range requested {
		reordered = append(reordered, byTag[tag])
	}
	sectionObj[key] = reordered
	return nil
}

func validateReorderTags(current []string, requested []string) error {
	if len(current) != len(requested) {
		return common.NewErrorf("reorder list length mismatch: got %d, want %d", len(requested), len(current))
	}
	expected := make(map[string]struct{}, len(current))
	for _, tag := range current {
		expected[tag] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	for _, tag := range requested {
		if _, exists := seen[tag]; exists {
			return common.NewErrorf("duplicate reorder tag: %s", tag)
		}
		seen[tag] = struct{}{}
		if _, ok := expected[tag]; !ok {
			return common.NewErrorf("unknown reorder tag: %s", tag)
		}
	}
	return nil
}

func ensureConfigObject(doc map[string]any, key string) map[string]any {
	obj, _ := doc[key].(map[string]any)
	if obj == nil {
		obj = map[string]any{}
		doc[key] = obj
	}
	return obj
}

func (s *ConfigService) preserveImplicitRouteFinal(tx *gorm.DB) error {
	doc, err := s.baseConfigDocument(tx)
	if err != nil {
		return err
	}
	route := ensureConfigObject(doc, "route")
	if final, _ := route["final"].(string); strings.TrimSpace(final) != "" {
		return nil
	}
	var firstTag string
	if err := tx.Model(model.Outbound{}).Select("tag").Order(entityorder.Clause).Limit(1).Scan(&firstTag).Error; err != nil {
		return err
	}
	if strings.TrimSpace(firstTag) == "" {
		return nil
	}
	route["final"] = firstTag
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return NewSingBoxBaseConfigStore(&s.SettingService).Save(tx, raw)
}

func preserveImplicitDNSFinal(doc map[string]any) error {
	dns := ensureConfigObject(doc, "dns")
	if final, _ := dns["final"].(string); strings.TrimSpace(final) != "" {
		return nil
	}
	servers, _ := dns["servers"].([]any)
	if len(servers) == 0 {
		return nil
	}
	firstServer, ok := servers[0].(map[string]any)
	if !ok {
		return common.NewError("config.dns.servers must contain JSON objects")
	}
	tag, _ := firstServer["tag"].(string)
	if strings.TrimSpace(tag) == "" {
		return nil
	}
	dns["final"] = tag
	return nil
}
