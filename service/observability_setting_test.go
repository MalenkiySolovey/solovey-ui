package service

import (
	"encoding/json"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

func TestObservabilityMemoryCapSettingValidation(t *testing.T) {
	settingService := initSettingTestDB(t)
	payload, err := json.Marshal(map[string]string{"observabilityMemoryCapMB": "0"})
	if err != nil {
		t.Fatal(err)
	}
	err = dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, payload)
	})
	if err == nil {
		t.Fatal("invalid observability memory cap should be rejected")
	}
}
