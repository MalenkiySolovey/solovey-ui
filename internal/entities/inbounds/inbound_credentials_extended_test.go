//go:build shtorm_extended

package entityinbounds

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strconv"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sshCredentialDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newInboundDB(t).Session(&gorm.Session{Logger: logger.Discard})
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func sshPrivateBundle(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "runtime-only-test-key")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func sshOptions(t *testing.T, mutation any) json.RawMessage {
	t.Helper()
	options := map[string]any{"listen": "127.0.0.1", "listen_port": 23456}
	if mutation != nil {
		options["host_key"] = mutation
	}
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func saveSSH(t *testing.T, db *gorm.DB, row *model.Inbound) error {
	t.Helper()
	return db.Transaction(func(tx *gorm.DB) error { return FillAndSave(tx, row, "") })
}

func storedSSHKey(t *testing.T, db *gorm.DB, row *model.Inbound) string {
	t.Helper()
	if err := db.First(row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(row.Options, &fields) != nil {
		t.Fatal("invalid stored SSH options")
	}
	var captured string
	if raw := fields["host_key"]; len(raw) > 0 && json.Unmarshal(raw, &captured) != nil {
		t.Fatal("host key was not sealed")
	}
	return captured
}

func sshView(t *testing.T, db *gorm.DB, row model.Inbound) secretref.View {
	t.Helper()
	projected, err := GetByIDs(db, strconv.Itoa(int(row.Id)))
	if err != nil || len(*projected) != 1 {
		t.Fatal("SSH normal read failed")
	}
	value, ok := (*projected)[0]["host_key"].(secretref.View)
	if !ok {
		t.Fatal("SSH normal read did not project SecretRef")
	}
	return value
}

func unchangedSSH(view secretref.View) any {
	return map[string]any{"intent": "unchanged", "reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision}
}

func TestSSHHostKeysSealMaskRetainRotateClearAndBindToRow(t *testing.T) {
	db := sshCredentialDB(t)
	bundle := sshPrivateBundle(t) + sshPrivateBundle(t)
	row := model.Inbound{Type: "ssh", Tag: "ssh-owned", Options: sshOptions(t, map[string]string{"intent": "replace", "value": bundle})}
	if err := saveSSH(t, db, &row); err != nil {
		t.Fatal("SSH key save failed", err)
	}
	captured := storedSSHKey(t, db, &row)
	if row.Id == 0 || !secretbox.IsEncrypted(captured) || bytes.Contains(row.Options, []byte("PRIVATE KEY")) || bytes.Contains(row.Options, []byte(bundle)) {
		t.Fatal("SSH host keys escaped sealed storage")
	}
	view := sshView(t, db, row)
	if !view.Present || len(view.RotationRevision) != 64 || view.Reference.Owner != "inbounds" {
		t.Fatal("host-key reference lost presence or identity")
	}
	projected, err := GetByIDs(db, strconv.Itoa(int(row.Id)))
	encoded, _ := json.Marshal(projected)
	if err != nil || bytes.Contains(encoded, []byte(bundle)) || bytes.Contains(encoded, []byte(captured)) {
		t.Fatal("SSH normal read disclosed key material")
	}
	rendered, err := marshalRuntimeInbound(db, row)
	var runtime struct {
		HostKeys []string `json:"host_key"`
	}
	if err != nil || json.Unmarshal(rendered, &runtime) != nil || len(runtime.HostKeys) != 2 || runtime.HostKeys[0]+runtime.HostKeys[1] != bundle {
		t.Fatal("scoped SSH render lost the private-key bundle")
	}
	for _, mutation := range []any{unchangedSSH(view), nil} {
		row.Options = sshOptions(t, mutation)
		if err := saveSSH(t, db, &row); err != nil || storedSSHKey(t, db, &row) != captured {
			t.Fatal("Keep or omission rotated SSH keys", err)
		}
	}
	row.Options = sshOptions(t, map[string]string{"intent": "replace", "value": sshPrivateBundle(t)})
	if err := saveSSH(t, db, &row); err != nil {
		t.Fatal("host-key rotation failed", err)
	}
	rotated := storedSSHKey(t, db, &row)
	if rotated == captured {
		t.Fatal("rotation did not replace the envelope")
	}
	row.Options = sshOptions(t, unchangedSSH(view))
	if err := saveSSH(t, db, &row); !errors.Is(err, secretref.ErrDenied) || storedSSHKey(t, db, &row) != rotated {
		t.Fatal("stale host-key draft was admitted")
	}
	copyRow := model.Inbound{Type: "ssh", Tag: "copied-envelope", Options: sshOptions(t, rotated)}
	if err := saveSSH(t, db, &copyRow); err == nil {
		t.Fatal("a client submitted raw ciphertext")
	}
	// Even a trusted raw database copy cannot grant runtime use on another row.
	copyRow.Id = 0
	if err := db.Create(&copyRow).Error; err != nil {
		t.Fatal(err)
	}
	if rendered, err := marshalRuntimeInbound(db, copyRow); err == nil || len(rendered) != 0 {
		t.Fatal("SSH envelope crossed its row binding")
	}
	row.Options = sshOptions(t, map[string]string{"intent": "clear"})
	if err := saveSSH(t, db, &row); err != nil || storedSSHKey(t, db, &row) != "" || sshView(t, db, row).Present {
		t.Fatal("Clear did not remove the stored host keys", err)
	}
	var events []model.AuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(events)
	if bytes.Contains(record, []byte("PRIVATE KEY")) || bytes.Contains(record, []byte(captured)) || bytes.Contains(record, []byte(rotated)) {
		t.Fatal("SSH credential audit disclosed material")
	}
}

func TestSSHHostKeyFailureRollsBackIdentityMasterAuditAndBlocksUse(t *testing.T) {
	db := sshCredentialDB(t)
	key := sshPrivateBundle(t)
	for _, mutation := range []any{key, []string{key}, map[string]string{"intent": "replace", "value": "private-key-canary"}, map[string]any{"intent": "clear", "value": nil}} {
		row := model.Inbound{Type: "ssh", Tag: "invalid-host-key", Options: sshOptions(t, mutation)}
		if err := saveSSH(t, db, &row); err == nil || bytes.Contains([]byte(err.Error()), []byte("private-key-canary")) || bytes.Contains([]byte(err.Error()), []byte(key)) {
			t.Fatal("invalid key input was admitted or disclosed")
		}
	}
	if err := db.Exec(`CREATE TRIGGER deny_ssh_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Inbound{Type: "ssh", Tag: "rollback-host-key", Options: sshOptions(t, map[string]string{"intent": "replace", "value": key})}
	if err := saveSSH(t, db, &row); err == nil {
		t.Fatal("audit rejection admitted a host-key write")
	}
	for _, record := range []any{&model.Inbound{}, &model.Setting{}, &model.AuditEvent{}} {
		var count int64
		if err := db.Model(record).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed host-key save left an identity, master or audit event")
		}
	}
	if err := db.Exec(`DROP TRIGGER deny_ssh_audit`).Error; err != nil {
		t.Fatal(err)
	}
	row.Id = 0
	if err := saveSSH(t, db, &row); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER deny_ssh_use BEFORE INSERT ON audit_events WHEN NEW.event = 'owner_secret_use' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if raw, err := marshalRuntimeInbound(db, row); err == nil || len(raw) != 0 {
		t.Fatal("SSH render bypassed synchronous use audit")
	}
}

func TestSSHLegacyHostKeyPresenceHasNoRuntimeAuthority(t *testing.T) {
	db := sshCredentialDB(t)
	for _, options := range []json.RawMessage{nil, json.RawMessage(`{}`), sshOptions(t, []string{sshPrivateBundle(t)})} {
		row := model.Inbound{Type: "ssh", Tag: "historical-" + strconv.Itoa(len(options)), Options: options}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		view := sshView(t, db, row)
		if view.RotationRevision != "" {
			t.Fatal("historical material was fingerprinted")
		}
		if view.Present {
			if raw, err := marshalRuntimeInbound(db, row); err == nil || len(raw) != 0 {
				t.Fatal("historical plaintext acquired runtime authority")
			}
		}
	}
}
