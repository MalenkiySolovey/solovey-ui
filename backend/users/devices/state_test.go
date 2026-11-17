package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/security/secretbox"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func deviceDB(t *testing.T) (*gorm.DB, model.Client, ResolvePolicy) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sql.Close() })
	if err := db.AutoMigrate(&model.Client{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	c := model.Client{Name: "device-owner", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	policy := func(_ *gorm.DB, id uint) (Policy, error) {
		return Policy{EndpointID: id, Available: true, ServerPrefix: netip.MustParsePrefix("10.77.0.1/24"), DeviceLimit: 2}, nil
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := Mutate(tx, Request{ClientID: c.Id, EndpointID: 9, Operation: "grant"}, policy, 1700000000)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return db, c, policy
}

func TestDeviceCreationIsReplaySafeAndNeverProjectsPrivateEnvelopes(t *testing.T) {
	db, c, policy := deviceDB(t)
	request := Request{ClientID: c.Id, EndpointID: 9, Operation: "create", RequestKey: "request-fixture-0001", Name: "laptop"}
	apply := func() error {
		return db.Transaction(func(tx *gorm.DB) error { _, err := Mutate(tx, request, policy, 1700000000); return err })
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	_, first, err := Load(db, c.Id)
	if err != nil || len(first.Devices) != 1 {
		t.Fatalf("state = %#v, %v", first, err)
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}
	stored, next, err := Load(db, c.Id)
	if err != nil || len(next.Devices) != 1 || next.Devices[0].ID != first.Devices[0].ID {
		t.Fatalf("replay changed identity: %#v, %v", next, err)
	}
	record := next.Devices[0]
	if !secretbox.IsEncrypted(record.PrivateKey) || !secretbox.IsEncrypted(record.PresharedKey) {
		t.Fatal("device credentials were not sealed")
	}
	projected, err := Project(c.Id, stored.Config)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(projected, []byte(record.PrivateKey)) || bytes.Contains(projected, []byte(record.PresharedKey)) {
		t.Fatal("ordinary user projection disclosed envelopes")
	}
	request.Name = "another-device"
	if err := apply(); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
}

func TestDeviceAuditFailureRollsBackKeyAndDatabaseMaster(t *testing.T) {
	db, c, policy := deviceDB(t)
	if err := db.Exec("CREATE TRIGGER fixture_deny_secret_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'fixture audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := Mutate(tx, Request{ClientID: c.Id, EndpointID: 9, Operation: "create", RequestKey: "request-fixture-0002", Name: "laptop"}, policy, 1700000000)
		return err
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	_, state, err := Load(db, c.Id)
	if err != nil || len(state.Devices) != 0 {
		t.Fatalf("failed audit leaked device state: %#v, %v", state, err)
	}
	var count int64
	if err := db.Model(&model.Setting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed audit leaked key lifecycle: %d, %v", count, err)
	}
}
