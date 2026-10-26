//go:build !shtorm_extended

package entityclients

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
)

func TestOfficialMasksAndLosslesslyPreservesSudokuDeliveryState(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "sealed", true: "historical-map"}[legacy], func(t *testing.T) {
			db := newClientDB(t)
			if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
				t.Fatal(err)
			}
			master, _ := credentials.GenerateSudokuMaster()
			split, _ := credentials.SplitSudokuMaster(master)
			bag, _ := json.Marshal(map[string]string{"1": split})
			ref, _ := ownerdb.Reference("users", 42, "sudoku.keys")
			mutation, _ := json.Marshal(map[string]string{"intent": "replace", "value": string(bag)})
			sealed, err := ownerdb.Apply(db, ref, "", mutation)
			if err != nil {
				t.Fatal("fixture sealing failed", err)
			}
			var captured any = sealed
			if legacy {
				captured = json.RawMessage(bag)
			}
			config, _ := json.Marshal(map[string]any{"sudoku": map[string]any{"keys": captured, "legacyNote": "preserved"}})
			row := model.Client{Id: 42, Name: "alice", Config: config, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-sudoku-subscription"}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			projected, err := GetByID(db, "42")
			if err != nil || len(*projected) != 1 {
				t.Fatal("historical read failed", err)
			}
			if bytes.Contains((*projected)[0].Config, []byte(sealed)) || bytes.Contains((*projected)[0].Config, []byte(split)) {
				t.Fatal("normal read disclosed private delivery state")
			}
			var view struct{ Sudoku struct{ Keys secretref.View } }
			_ = json.Unmarshal((*projected)[0].Config, &view)
			if !view.Sudoku.Keys.Present {
				t.Fatal("missing historical presence")
			}
			keep := map[string]any{"intent": "unchanged", "reference": view.Sudoku.Keys.Reference, "present": true, "rotationRevision": view.Sudoku.Keys.RotationRevision}
			for _, nextConfig := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
				if nextConfig == nil {
					nextConfig, _ = json.Marshal(map[string]any{"sudoku": map[string]any{"keys": keep, "legacyNote": "preserved"}})
				}
				row.Config, row.Desc = nextConfig, "metadata edit"
				data, _ := json.Marshal(row)
				_, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data})
				if legacy && !bytes.Equal(nextConfig, []byte(`{}`)) {
					// Historical plaintext has no safe rotation revision. The
					// shared secret contract requires replacement before Keep can
					// grant authority; omission still preserves the exact owner bytes.
					if err == nil {
						t.Fatal("unsealed historical reference acquired Keep authority")
					}
					continue
				}
				if err != nil {
					t.Fatal("unavailable metadata edit failed", err)
				}
				var actual model.Client
				if err := db.First(&actual, 42).Error; err != nil || !bytes.Equal(actual.Config, config) || actual.Desc != row.Desc {
					t.Fatal("metadata edit changed unavailable state")
				}
			}
			row.Config, _ = json.Marshal(map[string]any{"sudoku": map[string]any{"keys": map[string]string{"intent": "clear"}, "legacyNote": "preserved"}})
			data, _ := json.Marshal(row)
			if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err == nil {
				t.Fatal("Official changed unavailable delivery material")
			}
		})
	}
}

func TestOfficialRejectsNewSudokuDeliveryWithoutPrivateState(t *testing.T) {
	db := newClientDB(t)
	row := model.Client{Name: "alice", Config: json.RawMessage(`{"sudoku":{"keys":{"intent":"replace","value":"{}"}}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	data, _ := json.Marshal(row)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err == nil || !strings.Contains(err.Error(), "CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE") {
		t.Fatal("unavailable new delivery accepted")
	}
	var count int64
	_ = db.Model(&model.Client{}).Count(&count).Error
	if count != 0 || db.Migrator().HasTable(&model.Setting{}) || db.Migrator().HasTable(&model.AuditEvent{}) {
		t.Fatal("unavailable delivery created private owner state")
	}
}
