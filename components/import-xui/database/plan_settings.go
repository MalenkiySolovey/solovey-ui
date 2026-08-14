//go:build !minimal

package importxui

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"gorm.io/gorm"
)

func planSettings(ctx context.Context, tx *gorm.DB, src *source.Database, plan *MigrationPlan, strategy Strategy) error {
	settings, err := src.Settings()
	if err != nil {
		return err
	}
	for _, setting := range settings {
		if err := checkContext(ctx); err != nil {
			return err
		}
		target, ok := mapSettingKey(setting.Key)
		if !ok {
			// Surface every unmapped source setting as a skipped, warning-only
			// item so the operator can see exactly what did not migrate instead
			// of it being dropped silently. Most are compatible panel/xray specific keys
			// with no sing-box equivalent in s-ui.
			plan.Items = append(plan.Items, warningOnlyItem(KindSetting, setting.ID, setting.Key, setting.Key, []string{fmt.Sprintf("setting %s has no s-ui equivalent; not migrated", setting.Key)}))
			continue
		}
		preview := model.Setting{Key: target, Value: setting.Value}
		previewJSON, err := marshalJSON(preview)
		if err != nil {
			return err
		}
		conflict, err := recordExists(tx, &model.Setting{}, "key = ?", target)
		if err != nil {
			return err
		}
		action := defaultAction(conflict, strategy)
		var warnings []string
		if isHostSpecificSettingKey(setting.Key) {
			// Default to skip so a migration to a different host/domain does not
			// overwrite this server's working bind/port/domain/cert settings and
			// break it. The item stays in the plan; the operator can re-enable it
			// when migrating within the same host.
			action = ActionSkip
			warnings = []string{fmt.Sprintf("setting %s is server-specific (listen address, port, domain or TLS certificate path); skipped by default to avoid breaking this host - enable it only when migrating to the same host/domain", setting.Key)}
		}
		plan.Items = append(plan.Items, PlanItem{
			Kind:        KindSetting,
			SrcID:       setting.ID,
			SrcTag:      setting.Key,
			DstTag:      target,
			Action:      action,
			Conflict:    conflict,
			PreviewJSON: previewJSON,
			Warnings:    warnings,
		})
	}
	return nil
}

func (s *applyState) applySettings(ctx context.Context, tx *gorm.DB, src *source.Database) error {
	if !s.hasKind(KindSetting) {
		return nil
	}
	settings, err := src.Settings()
	if err != nil {
		return err
	}
	selected := make(map[string]string)
	var progressTargets []string
	for _, setting := range settings {
		if err := checkContext(ctx); err != nil {
			return err
		}
		target, ok := mapSettingKey(setting.Key)
		if !ok {
			continue
		}
		item := s.item(KindSetting, setting.ID)
		if item.Action == ActionSkip {
			continue
		}
		if item.DstTag != "" && item.DstTag != target {
			return ErrPlanInvalid
		}
		selected[target] = setting.Value
		progressTargets = append(progressTargets, target)
	}
	if len(selected) == 0 {
		return nil
	}
	raw, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	if err := (&service.SettingService{}).Save(tx, raw); err != nil {
		return err
	}
	for _, target := range progressTargets {
		s.progress("settings", target)
	}
	return nil
}

// xuiSettingKeyMap maps compatible panel setting keys to their s-ui equivalents. It only
// contains core settings whose meaning is portable to s-ui's sing-box-based
// panel: network/listen/port/path/domain/cert settings, subscription endpoints,
// and a few display toggles. Optional setting owners contribute their own
// external aliases through service.SettingContribution. Xray-specific subscription
// payload settings (subJson*/subClash* fragments, routing rules, encode mode)
// and compatible panel-only keys are intentionally omitted because their format/semantics
// differ; planSettings reports those as skipped so the loss is visible.
var xuiSettingKeyMap = map[string]string{
	// Web panel
	"webListen":   "webListen",
	"webDomain":   "webDomain",
	"webPort":     "webPort",
	"webCertFile": "webCertFile",
	"webKeyFile":  "webKeyFile",
	"webBasePath": "webPath", // renamed in s-ui

	// Subscription service
	"subListen":      "subListen",
	"subPort":        "subPort",
	"subPath":        "subPath",
	"subDomain":      "subDomain",
	"subCertFile":    "subCertFile",
	"subKeyFile":     "subKeyFile",
	"subURI":         "subURI",
	"subJsonPath":    "subJsonPath",
	"subClashPath":   "subClashPath",
	"subXrayPath":    "subXrayPath",
	"subJsonURI":     "subJsonURI",
	"subClashURI":    "subClashURI",
	"subXrayURI":     "subXrayURI",
	"subJsonEnable":  "subJsonEnable",
	"subClashEnable": "subClashEnable",
	"subXrayEnable":  "subXrayEnable",
	"subShowInfo":    "subShowInfo",
	"subTitle":       "subTitle",
	"subSupportUrl":  "subSupportUrl",
	"subProfileUrl":  "subProfileUrl",
	"subAnnounce":    "subAnnounce",
	"subUpdates":     "subUpdates",

	// Panel behavior
	"timeLocation":  "timeLocation",
	"sessionMaxAge": "sessionMaxAge",
}

// hostSpecificSettingKeys are compatible panel source keys whose values identify the
// SOURCE server's host/domain: the bind address, the panel/sub domain, on-disk
// TLS certificate paths, and the absolute subscription URLs that embed the
// host. Copying these onto a different destination host breaks it - the panel
// would bind an IP that does not exist here, present a stale domain, reference
// certificate files that are absent, or hand out subscription links pointing at
// the old server. planSettings keeps them visible in the plan but defaults them
// to skip, so a cross-host/domain migration is safe; the operator can still opt
// in when moving within the same host/domain.
//
// Ports (webPort/subPort) are intentionally NOT here: they are logical config a
// migration is expected to carry over, and binding a different port is not a
// host/domain mismatch. The operator can still untick them in the plan review.
var hostSpecificSettingKeys = map[string]struct{}{
	"webListen":   {},
	"webDomain":   {},
	"webCertFile": {},
	"webKeyFile":  {},
	"subListen":   {},
	"subDomain":   {},
	"subCertFile": {},
	"subKeyFile":  {},
	"subURI":      {},
	"subJsonURI":  {},
	"subClashURI": {},
	"subXrayURI":  {},
}

func isHostSpecificSettingKey(key string) bool {
	_, ok := hostSpecificSettingKeys[key]
	return ok
}

func mapSettingKey(key string) (string, bool) {
	target, ok := xuiSettingKeyMap[key]
	if ok {
		return target, true
	}
	target, ok = service.CurrentSettingImportAliases()[key]
	return target, ok
}
