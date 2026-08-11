package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const OperationsLifecycleStepID = "core-1.11-operations-lifecycle"

var OperationsLifecycleChecksum = operationsLifecycleSchemaChecksum()

func operationsLifecycleModels() []any {
	return []any{
		&model.UpdateReleaseState{}, &model.UpdateOperation{}, &model.UpdateJournal{},
		&model.ResourcePressureState{}, &model.ResourcePressureTransition{},
		&model.MigrationJournal{}, &model.DataLifecycleOperation{}, &model.DataLifecycleJournal{},
	}
}

func addOperationsLifecycleSchema(tx *gorm.DB) error {
	return tx.AutoMigrate(operationsLifecycleModels()...)
}

func operationsLifecycleSchemaChecksum() string {
	type field struct {
		Name string `json:"name"`
		Type string `json:"type"`
		GORM string `json:"gorm"`
	}
	type table struct {
		Name   string  `json:"name"`
		Fields []field `json:"fields"`
	}
	contract := struct {
		Schema string  `json:"schema"`
		Tables []table `json:"tables"`
	}{Schema: "solovey.core-schema/1.11", Tables: []table{}}
	for _, value := range operationsLifecycleModels() {
		typeValue := reflect.TypeOf(value).Elem()
		tableName := reflect.ValueOf(value).MethodByName("TableName").Call(nil)[0].String()
		item := table{Name: tableName, Fields: make([]field, 0, typeValue.NumField())}
		for index := 0; index < typeValue.NumField(); index++ {
			modelField := typeValue.Field(index)
			item.Fields = append(item.Fields, field{Name: modelField.Name, Type: modelField.Type.String(), GORM: modelField.Tag.Get("gorm")})
		}
		contract.Tables = append(contract.Tables, item)
	}
	encoded, _ := json.Marshal(contract)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
