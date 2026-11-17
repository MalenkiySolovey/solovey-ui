package ownerdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/secretref"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/backend/settings/store"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func candidateDB(t *testing.T, withAudit bool) *gorm.DB {
	t.Helper()
	file := filepath.Join(t.TempDir(), "candidate.db")
	db, err := gorm.Open(sqlite.Open(file), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if withAudit {
		if err := db.AutoMigrate(&model.AuditEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestSecretMutationRollsBackMasterWhenAuditAdmissionFails(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	db := candidateDB(t, false)
	ref, err := Reference("users", 42, "credential")
	if err != nil {
		t.Fatal(err)
	}
	value, err := Apply(db, ref, "", json.RawMessage(`{"intent":"replace","value":"synthetic-owned-credential"}`))
	if !errors.Is(err, secretref.ErrUnavailable) || value != "" {
		t.Fatal("failed audit admitted a secret mutation")
	}
	var count int64
	if err := db.Model(&model.Setting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed mutation published a master: count=%d err=%v", count, err)
	}
}

func TestSecretReadUsesExactCandidateMasterAndAuditsBeforeBorrow(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "")
	db, foreign := candidateDB(t, true), candidateDB(t, true)
	if _, err := settingsstore.EnsureMasterSecret(foreign); err != nil {
		t.Fatal(err)
	}
	ref, err := Reference("users", 42, "credential")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := Apply(db, ref, "", json.RawMessage(`{"intent":"replace","value":"synthetic-owned-credential"}`))
	if err != nil {
		t.Fatal(err)
	}
	request := secretref.Access{Reference: ref, Actor: secretref.Actor{Component: "users"}, Operation: secretref.UseForOwner, IntentID: "runtime-config"}
	calls := 0
	var borrowed []byte
	_, err = Access(context.Background(), db, request, captured, func(value []byte) error {
		calls++
		var count int64
		if err := db.Model(&model.AuditEvent{}).Where("event=?", "owner_secret_use").Count(&count).Error; err != nil || count != 1 {
			t.Fatal("plaintext reached owner before durable use audit")
		}
		if string(value) != "synthetic-owned-credential" {
			t.Fatal("candidate decrypted wrong owner value")
		}
		borrowed = value
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("candidate access failed: %v", err)
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("borrowed plaintext survived callback scope")
	}
	_, err = Access(context.Background(), foreign, request, captured, func([]byte) error { calls++; return nil })
	if !errors.Is(err, secretref.ErrUnavailable) || calls != 1 {
		t.Fatal("foreign candidate reused another database's key or leaked plaintext")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Access(cancelled, db, request, captured, func([]byte) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled secret access reached owner effect")
	}
}
