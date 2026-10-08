package singboxconfig

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"gorm.io/gorm"
)

// StageStoredBaseUpgrade receives the complete owner-assembled projection so
// HTTP aliases and references use the real outbound catalogue. It persists only
// this owner's base sections; entity rows remain their owners' authority.
func StageStoredBaseUpgrade(tx *gorm.DB, complete []byte) error {
	var setting model.Setting
	result := tx.Where("key = ?", "config").Limit(1).Find(&setting)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	var source, candidate map[string]json.RawMessage
	if err := json.Unmarshal([]byte(setting.Value), &source); err != nil {
		return err
	}
	if err := json.Unmarshal(complete, &candidate); err != nil {
		return err
	}
	for _, key := range []string{"inbounds", "outbounds", "endpoints", "services", "certificate_providers"} {
		delete(candidate, key)
	}
	// Entity sections were never consumed from base storage. Keep their exact
	// historical bytes as dormant state, rather than silently discarding them.
	for _, key := range []string{"inbounds", "outbounds", "endpoints", "services", "certificate_providers"} {
		if raw, present := source[key]; present {
			candidate[key] = raw
		}
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	if bytes.Equal(data, []byte(setting.Value)) || equalBaseJSON([]byte(setting.Value), data) {
		return nil
	}
	return tx.Model(&model.Setting{}).Where("key = ?", "config").Update("value", string(data)).Error
}

func equalBaseJSON(left, right []byte) bool {
	var a, b any
	for _, entry := range []struct {
		data   []byte
		result *any
	}{{left, &a}, {right, &b}} {
		decoder := json.NewDecoder(bytes.NewReader(entry.data))
		decoder.UseNumber()
		if decoder.Decode(entry.result) != nil {
			return false
		}
	}
	return reflect.DeepEqual(a, b)
}

func StageBaseOptionsUpgrade(tx *gorm.DB) ([]diagnostics.Finding, error) {
	if !tx.Migrator().HasTable(&model.Setting{}) {
		return nil, nil
	}
	var setting model.Setting
	result := tx.Where("key = ?", "config").Limit(1).Find(&setting)
	if result.Error != nil || result.RowsAffected == 0 || setting.Value == "" {
		return nil, result.Error
	}
	prepared, err := PrepareBaseOptionsUpgrade([]byte(setting.Value))
	if err != nil {
		return prepared.Findings, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(setting.Value), &fields)
	for _, key := range []string{"inbounds", "outbounds", "endpoints", "services"} {
		if raw, present := fields[key]; present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && !bytes.Equal(bytes.TrimSpace(raw), []byte("[]")) {
			prepared.Findings = append(prepared.Findings, diagnostics.Finding{Kind: "config", Path: "config." + key, Code: "BASE_ENTITY_SECTION_DORMANT", Severity: diagnostics.Warn, Message: "This historical base section remains preserved. Entity tables own the active runtime section and have always replaced this embedded section during assembly.", MigrationOutcome: diagnostics.AutomaticDiagnostic, AutomaticAvailable: true})
		}
	}
	if !bytes.Equal(prepared.Candidate, []byte(setting.Value)) {
		err = tx.Model(&model.Setting{}).Where("key = ?", "config").Update("value", string(prepared.Candidate)).Error
	}
	return prepared.Findings, err
}
