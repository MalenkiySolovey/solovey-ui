//go:build shtorm_extended

package entityinbounds

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	sudokucrypto "github.com/sagernet/sing-box/transport/sudoku/crypto"
	"gorm.io/gorm"
)

func sudokuOptions(mutation any, public string) json.RawMessage {
	fields := map[string]any{"listen": "127.0.0.1", "listen_port": 23461, "disable_http_mask": true}
	if mutation != nil {
		fields["master_key"] = mutation
	}
	if public != "" {
		fields["key"] = public
	}
	raw, _ := json.Marshal(fields)
	return raw
}

func TestSudokuMasterIsSealedBoundAndOnlyPublicKeyReachesRuntime(t *testing.T) {
	db := sshCredentialDB(t)
	master, _ := credentials.GenerateSudokuMaster()
	public, _ := credentials.SudokuPublicKey(master)
	row := model.Inbound{Type: "sudoku", Tag: "sudoku-owned", Options: sudokuOptions(map[string]string{"intent": "replace", "value": master}, "")}
	save := func() error { return db.Transaction(func(tx *gorm.DB) error { return FillAndSave(tx, &row, "") }) }
	if err := save(); err != nil {
		t.Fatal("master save failed", err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Options, &fields)
	var captured, savedPublic string
	_ = json.Unmarshal(fields["master_key"], &captured)
	_ = json.Unmarshal(fields["key"], &savedPublic)
	if row.Id == 0 || !secretbox.IsEncrypted(captured) || savedPublic != public || bytes.Contains(row.Options, []byte(master)) {
		t.Fatal("master escaped its encrypted Inbounds row")
	}
	projected, err := GetByIDs(db, strconv.Itoa(int(row.Id)))
	if err != nil || len(*projected) != 1 {
		t.Fatal("master presence read failed")
	}
	view := (*projected)[0]["master_key"].(secretref.View)
	encoded, _ := json.Marshal(projected)
	if !view.Present || view.Reference.Owner != "inbounds" || bytes.Contains(encoded, []byte(captured)) || bytes.Contains(encoded, []byte(master)) {
		t.Fatal("normal read disclosed master material")
	}
	runtime, err := marshalRuntimeInbound(db, row)
	if err != nil || bytes.Contains(runtime, []byte("master_key")) || !bytes.Contains(runtime, []byte(public)) {
		t.Fatal("private master reached runtime or public derivation was lost")
	}
	split, _ := credentials.SplitSudokuMaster(master)
	recovered, err := sudokucrypto.RecoverPublicKey(split)
	if err != nil || sudokucrypto.EncodePoint(recovered) != public {
		t.Fatal("Panel delivery key differs from the pinned engine consumer")
	}
	fact := registry.Resolve("inbounds", "sudoku")
	if !fact.Available() || fact.AuthenticatedUsers || (StoredUsers{}).HasUser("sudoku") {
		t.Fatal("Sudoku acquired fictional authenticated user semantics")
	}
	for _, mutation := range []any{nil, unchangedSSH(view)} {
		row.Options = sudokuOptions(mutation, public)
		if err := save(); err != nil {
			t.Fatal("Keep failed", err)
		}
		var next map[string]json.RawMessage
		_ = json.Unmarshal(row.Options, &next)
		var kept string
		_ = json.Unmarshal(next["master_key"], &kept)
		if kept != captured {
			t.Fatal("metadata save rotated master")
		}
	}
	rotated, _ := credentials.GenerateSudokuMaster()
	row.Options = sudokuOptions(map[string]string{"intent": "replace", "value": rotated}, public)
	if err := save(); err != nil {
		t.Fatal("master rotation failed", err)
	}
	row.Options = sudokuOptions(unchangedSSH(view), public)
	if err := save(); err == nil {
		t.Fatal("stale editor retained authority after rotation")
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	var current map[string]json.RawMessage
	_ = json.Unmarshal(row.Options, &current)
	var currentPublic string
	_ = json.Unmarshal(current["key"], &currentPublic)
	row.Options = sudokuOptions(map[string]string{"intent": "clear"}, currentPublic)
	if err := save(); err != nil {
		t.Fatal("master clear failed", err)
	}
	if _, err := marshalRuntimeInbound(db, row); err != nil {
		t.Fatal("clearing master deleted the public serving identity", err)
	}
	if err := credentials.UseSudokuMaster(t.Context(), db, row, "cleared-master", func([]byte) error { t.Fatal("cleared master remained available"); return nil }); err == nil {
		t.Fatal("cleared master was available for new delivery")
	}
}

func TestSudokuDefaultGenerationAndFailedCandidateRollBackOwnerMaterial(t *testing.T) {
	db := sshCredentialDB(t)
	row := model.Inbound{Type: "sudoku", Tag: "generated-sudoku", Options: sudokuOptions(nil, "")}
	if err := db.Transaction(func(tx *gorm.DB) error { return FillAndSave(tx, &row, "") }); err != nil {
		t.Fatal("default master was not generated", err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Options, &fields)
	var captured string
	_ = json.Unmarshal(fields["master_key"], &captured)
	if !secretbox.IsEncrypted(captured) {
		t.Fatal("default master is not sealed")
	}
	clone := row
	clone.Id = 0
	clone.Tag = "forged-owner"
	if err := db.Transaction(func(tx *gorm.DB) error { return FillAndSave(tx, &clone, "") }); err == nil {
		t.Fatal("foreign ciphertext acquired a new owner")
	}
	var count int64
	_ = db.Model(&model.Inbound{}).Count(&count).Error
	if count != 1 {
		t.Fatal("failed master save left a placeholder row")
	}
	for _, value := range []string{"private-sentinel", "0000000000000000000000000000000000000000000000000000000000000000"} {
		clone = model.Inbound{Type: "sudoku", Tag: "bad-master", Options: sudokuOptions(map[string]string{"intent": "replace", "value": value}, "")}
		if err := db.Transaction(func(tx *gorm.DB) error { return FillAndSave(tx, &clone, "") }); err == nil {
			t.Fatal("invalid master accepted")
		}
	}
}
