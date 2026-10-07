package service

import (
	"bytes"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	formats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	"testing"
)

func TestHTTPProjectionLateDNSFailureKeepsWholePreimage(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	old := `{"dns":{"servers":[{"tag":"a","address":"local"}]},"route":{"final":"direct","rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"example","action":"reject"}]}],"rule_set":[{"type":"remote","tag":"operator","url":"https://operator.example/custom.srs","download_detour":"direct"}]}}`
	if err := db.Create(&model.Setting{Key: "config", Value: old}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(db, "", true)
	if err == nil || diagnostics.FirstError(result.DNSCompatibility) == nil || len(result.HTTPCompatibility) == 0 {
		t.Fatal("late owner failure not detected")
	}
	var root struct {
		HTTPClients []json.RawMessage `json:"http_clients"`
		Route       struct {
			Rules   []struct{ Rules []struct{ Action string } } `json:"rules"`
			RuleSet []struct {
				Detour string `json:"download_detour"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	if json.Unmarshal(result.Config, &root) != nil || len(root.HTTPClients) != 0 || len(root.Route.RuleSet) != 1 || root.Route.RuleSet[0].Detour != "direct" {
		t.Fatal("partial HTTP candidate escaped")
	}
	if len(root.Route.Rules) != 1 || len(root.Route.Rules[0].Rules) != 1 || root.Route.Rules[0].Rules[0].Action != "reject" {
		t.Fatal("late failure returned partial W1 rule conversion")
	}
	var row model.Setting
	if err := db.Where("key = ?", "config").Take(&row).Error; err != nil || row.Value != old {
		t.Fatal("source setting was mutated")
	}
}

func TestSubscriptionTemplateSaveSharesStrictValidationAndPreservesSource(t *testing.T) {
	settings := initSettingTestDB(t)
	db := dbsqlite.DB()
	old := `{"rule_set":[{"type":"remote","tag":"operator","url":"https://operator.example/custom.srs","download_detour":"direct"}],"default_domain_resolver":{"server":"missing","disable_cache":false,"rewrite_ttl":0}}`
	if err := db.Create(&model.Setting{Key: settingKeySubJsonExt, Value: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := settings.saveSetting(settingKeySubJsonExt, old); err == nil {
		t.Fatal("strict single-key save admitted unsafe template")
	}
	payload, _ := json.Marshal(map[string]string{settingKeySubJsonExt: old})
	if err := settings.Save(db, payload); err == nil {
		t.Fatal("strict batch save admitted unsafe template")
	}
	var row model.Setting
	if err := db.Where("key = ?", settingKeySubJsonExt).Take(&row).Error; err != nil || row.Value != old {
		t.Fatal("failed save lost template")
	}
	fixed := `{"rule_set":[{"type":"remote","tag":"operator","url":"https://operator.example/custom.srs","download_detour":"direct"}],"dns":{"servers":[{"type":"local","tag":"selected"}]},"default_domain_resolver":{"server":"selected","disable_cache":false,"rewrite_ttl":0}}`
	prepared, err := formats.PrepareJSONExtensionUpgrade([]byte(fixed))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ = json.Marshal(map[string]string{settingKeySubJsonExt: string(prepared.Candidate)})
	if err := settings.Save(db, payload); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", settingKeySubJsonExt).Take(&row).Error; err != nil || !bytes.Contains([]byte(row.Value), []byte(`"disable_cache":false`)) || !bytes.Contains([]byte(row.Value), []byte(`"rewrite_ttl":0`)) {
		t.Fatal("corrected save erased resolver intent")
	}
}
