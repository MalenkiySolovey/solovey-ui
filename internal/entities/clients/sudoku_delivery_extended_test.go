//go:build shtorm_extended && with_quic

package entityclients

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func sudokuDeliveryInbound(t *testing.T, db *gorm.DB, id uint, master string) model.Inbound {
	t.Helper()
	public, err := credentials.SudokuPublicKey(master)
	if err != nil {
		t.Fatal("public derivation failed")
	}
	ref, _ := ownerdb.Reference("inbounds", id, "master_key")
	mutation, _ := json.Marshal(map[string]string{"intent": "replace", "value": master})
	sealed, err := ownerdb.Apply(db, ref, "", mutation)
	if err != nil {
		t.Fatal("master sealing failed", err)
	}
	options, _ := json.Marshal(map[string]string{"key": public, "master_key": sealed})
	row := model.Inbound{Id: id, Type: "sudoku", Tag: "sudoku-" + strconv.Itoa(int(id)), Options: options}
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func sudokuDeliveryBag(t *testing.T, db *gorm.DB, row model.Client) (string, map[string]string) {
	t.Helper()
	var root map[string]json.RawMessage
	var fields struct{ Keys string }
	if json.Unmarshal(row.Config, &root) != nil || json.Unmarshal(root["sudoku"], &fields) != nil || !secretbox.IsEncrypted(fields.Keys) {
		t.Fatal("delivery keys are not encrypted")
	}
	var keys map[string]string
	if err := credentials.UseClient(t.Context(), db, row.Id, "sudoku", root["sudoku"], "sudoku-test-delivery", func(value []byte) error {
		var delivery struct{ Keys map[string]string }
		if json.Unmarshal(value, &delivery) != nil {
			t.Fatal("delivery bundle is not canonical JSON")
		}
		keys = delivery.Keys
		return nil
	}); err != nil {
		t.Fatal("owner could not consume delivery bundle", err)
	}
	return fields.Keys, keys
}

func TestSudokuDeliveryKeysAreSealedRetainedAndRotateWithServingIdentity(t *testing.T) {
	db := managedClientDB(t)
	master, _ := credentials.GenerateSudokuMaster()
	inbound := sudokuDeliveryInbound(t, db, 1, master)
	client := model.Client{Name: "alice", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`)}
	save := func(action string) error {
		data, _ := json.Marshal(client)
		_, err := Save(SaveRequest{Tx: db, Action: action, Data: data})
		return err
	}
	if err := save("new"); err != nil {
		t.Fatal("new assignment failed", err)
	}
	if err := db.Where("name = ?", "alice").First(&client).Error; err != nil {
		t.Fatal(err)
	}
	sealed, keys := sudokuDeliveryBag(t, db, client)
	if len(keys) != 1 || !credentials.SudokuSplitMatches(keys["1"], master) {
		t.Fatal("assignment did not derive the serving key")
	}
	projected, err := GetByID(db, strconv.Itoa(int(client.Id)))
	if err != nil || len(*projected) != 1 {
		t.Fatal("masked read failed", err)
	}
	if bytes.Contains((*projected)[0].Config, []byte(sealed)) || bytes.Contains((*projected)[0].Config, []byte(keys["1"])) {
		t.Fatal("normal read disclosed a delivery credential")
	}
	var view struct{ Sudoku struct{ Keys secretref.View } }
	_ = json.Unmarshal((*projected)[0].Config, &view)
	if !view.Sudoku.Keys.Present || view.Sudoku.Keys.Reference.Owner != "users" {
		t.Fatal("missing Users-owned presence")
	}
	keep := map[string]any{"intent": "unchanged", "reference": view.Sudoku.Keys.Reference, "present": true, "rotationRevision": view.Sudoku.Keys.RotationRevision}
	setMutation := func(mutation any) {
		client.Config, _ = json.Marshal(map[string]any{"sudoku": map[string]any{"keys": mutation}})
	}
	setMutation(keep)
	client.Enable = false
	if err := save("edit"); err != nil {
		t.Fatal("metadata Keep failed", err)
	}
	_ = db.First(&client, client.Id).Error
	if next, _ := sudokuDeliveryBag(t, db, client); next != sealed {
		t.Fatal("disabling a Panel row rotated a shared protocol identity")
	}
	client.Inbounds, client.Config = json.RawMessage(`[]`), json.RawMessage(`{}`)
	if err := save("edit"); err != nil {
		t.Fatal("unassignment failed", err)
	}
	_ = db.First(&client, client.Id).Error
	if next, retained := sudokuDeliveryBag(t, db, client); next != sealed || retained["1"] != keys["1"] {
		t.Fatal("unassignment erased delivery history")
	}
	client.Inbounds = json.RawMessage(`[1]`)
	if err := save("edit"); err != nil {
		t.Fatal("reassignment failed", err)
	}
	_ = db.First(&client, client.Id).Error
	wrongMaster, _ := credentials.GenerateSudokuMaster()
	wrongSplit, _ := credentials.SplitSudokuMaster(wrongMaster)
	bag, _ := json.Marshal(map[string]string{"1": wrongSplit})
	setMutation(map[string]string{"intent": "replace", "value": string(bag)})
	if err := save("edit"); err == nil {
		t.Fatal("explicit wrong-master split was accepted")
	}
	_ = db.First(&client, client.Id).Error
	if next, _ := sudokuDeliveryBag(t, db, client); next != sealed {
		t.Fatal("failed replacement changed the bundle")
	}
	// A serving-key rotation refreshes assigned keys through the existing
	// inbound-to-Users hook; no independently authenticated engine user exists.
	inbound = sudokuDeliveryInbound(t, db, 1, wrongMaster)
	if err := db.Transaction(func(tx *gorm.DB) error {
		rows := []model.Inbound{inbound}
		return UpdateLinksByInboundChange(tx, &rows, "", inbound.Tag)
	}); err != nil {
		t.Fatal("master rotation hook failed", err)
	}
	_ = db.First(&client, client.Id).Error
	rotated, nextKeys := sudokuDeliveryBag(t, db, client)
	if rotated == sealed || !credentials.SudokuSplitMatches(nextKeys["1"], wrongMaster) || credentials.SudokuSplitMatches(nextKeys["1"], master) {
		t.Fatal("delivery identity did not follow master rotation")
	}
	setMutation(keep)
	if err := save("edit"); err == nil {
		t.Fatal("stale Keep overwrote a rotated bundle")
	}
	_ = db.First(&client, client.Id).Error
	// Clearing the private master preserves already delivered matching keys.
	var options map[string]json.RawMessage
	_ = json.Unmarshal(inbound.Options, &options)
	delete(options, "master_key")
	inbound.Options, _ = json.Marshal(options)
	if err := db.Save(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client.Config = json.RawMessage(`{}`)
	if err := save("edit"); err != nil {
		t.Fatal("master clear invalidated existing delivery", err)
	}
	_ = db.First(&client, client.Id).Error
	if next, _ := sudokuDeliveryBag(t, db, client); next != rotated {
		t.Fatal("master clear rewrote an existing split")
	}
	setMutation(map[string]string{"intent": "clear"})
	if err := save("edit"); err != nil {
		t.Fatal("explicit delivery clear failed", err)
	}
	_ = db.First(&client, client.Id).Error
	client.Config = json.RawMessage(`{}`)
	if err := save("edit"); err != nil {
		t.Fatal("metadata save resurrected cleared delivery", err)
	}
	_ = db.First(&client, client.Id).Error
	var cleared struct{ Sudoku struct{ Keys string } }
	_ = json.Unmarshal(client.Config, &cleared)
	if cleared.Sudoku.Keys != "" {
		t.Fatal("cleared split keys reappeared")
	}
}

func TestSudokuForeignReferenceAndFailedAssignmentLeaveNoPrivateOwnerMutation(t *testing.T) {
	db := managedClientDB(t)
	master, _ := credentials.GenerateSudokuMaster()
	sudokuDeliveryInbound(t, db, 1, master)
	client := model.Client{Name: "missing-master", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[2]`), Links: json.RawMessage(`[]`)}
	public, _ := credentials.SudokuPublicKey(master)
	options, _ := json.Marshal(map[string]string{"key": public})
	if err := db.Create(&model.Inbound{Id: 2, Type: "sudoku", Tag: "public-only", Options: options}).Error; err != nil {
		t.Fatal(err)
	}
	var beforeAudit int64
	_ = db.Model(&model.AuditEvent{}).Count(&beforeAudit).Error
	data, _ := json.Marshal(client)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err == nil {
		t.Fatal("missing private master granted new delivery")
	}
	var clients, afterAudit int64
	_ = db.Model(&model.Client{}).Count(&clients).Error
	_ = db.Model(&model.AuditEvent{}).Count(&afterAudit).Error
	if clients != 0 || afterAudit != beforeAudit {
		t.Fatal("failed assignment left a row or audit mutation")
	}
	client.Name, client.Inbounds = "valid", json.RawMessage(`[1]`)
	data, _ = json.Marshal(client)
	ids, err := Save(SaveRequest{Tx: db, Action: "new", Data: data})
	if err != nil || len(ids) != 1 {
		t.Fatal("valid assignment failed", err)
	}
	_ = db.Where("name = ?", "valid").First(&client).Error
	clientID := client.Id
	sealed, _ := sudokuDeliveryBag(t, db, client)
	clone := client
	clone.Id, clone.Name = 0, "forged-row"
	data, _ = json.Marshal(clone)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err == nil {
		t.Fatal("foreign ciphertext acquired a new Users owner")
	}
	_ = db.Model(&model.Client{}).Count(&clients).Error
	if clients != 1 {
		t.Fatal("failed foreign assignment left a placeholder")
	}
	_ = db.First(&client, clientID).Error
	if current, _ := sudokuDeliveryBag(t, db, client); current != sealed {
		t.Fatal("foreign assignment changed original material")
	}
}
