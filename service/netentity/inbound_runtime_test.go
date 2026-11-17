package netentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/entityidentity"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type inboundRuntimeFixture struct {
	calls           int
	config          []byte
	bindings        []entityidentity.InboundBinding
	removed, closed string
	failure         error
}

func (f *inboundRuntimeFixture) IsRunning() bool {
	if f == nil {
		panic("typed-nil runtime invoked")
	}
	return true
}
func (f *inboundRuntimeFixture) AddInboundWithBindings(config []byte, bindings []entityidentity.InboundBinding) error {
	f.calls++
	f.config = append([]byte(nil), config...)
	f.bindings = append([]entityidentity.InboundBinding(nil), bindings...)
	return f.failure
}
func (f *inboundRuntimeFixture) RemoveInbound(tag string) error     { f.removed = tag; return f.failure }
func (f *inboundRuntimeFixture) CloseInboundConnections(tag string) { f.closed = tag }

func TestInboundRuntimeRejectsMissingAndTypedNilOwner(t *testing.T) {
	var typedNil *inboundRuntimeFixture
	for _, owner := range []InboundRuntime{nil, typedNil} {
		service := &InboundService{Core: owner}
		if adapter := service.inboundCoreFromDB(nil); adapter != nil {
			t.Fatal("missing runtime produced mutation adapter")
		}
		if err := service.RestartInbounds(nil, []uint{1}); err != nil {
			t.Fatal("missing runtime changed existing no-op", err)
		}
	}
}

func TestInboundRuntimeBindsCredentialsFromTheSuppliedTransaction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	owner := &inboundRuntimeFixture{}
	config := []byte(`{"type":"anytls","tag":"in","users":[{"name":"proven-principal","password":"fixture-only"}]}`)
	rollback := errors.New("fixture transaction rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.Inbound{Id: 1, Tag: "in", Type: "anytls"}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Client{Id: 7, Enable: true, Name: "display is not authority", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"anytls":{"name":"proven-principal","password":"fixture-only"}}`)}).Error; err != nil {
			return err
		}
		adapter := (&InboundService{Core: owner}).inboundCoreFromDB(tx)
		if err := adapter.AddInbound(config); err != nil {
			return err
		}
		if owner.calls != 1 || !bytes.Equal(owner.config, config) || len(owner.bindings) != 1 || owner.bindings[0].Inbound != "in" || owner.bindings[0].Principal != "proven-principal" || owner.bindings[0].VPNUserID() != entityidentity.VPNUserID(7) {
			t.Fatal("binding did not use the exact candidate view", owner)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.Client{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("adapter committed Panel state independently", count, err)
	}
}

func TestInboundRuntimeRejectionPrecedesEffectAndOwnerErrorsRemainExact(t *testing.T) {
	failure := errors.New("runtime mutation rejected")
	owner := &inboundRuntimeFixture{failure: failure}
	adapter := inboundCoreAdapter{core: owner}
	for _, config := range [][]byte{[]byte(`{`), []byte(`{"type":"anytls","tag":"in","users":[{"name":"unproven","password":"fixture"}]}`)} {
		if err := adapter.AddInbound(config); err == nil || owner.calls != 0 {
			t.Fatal("unproven identity reached runtime", err, owner.calls)
		}
	}
	if err := adapter.AddInbound([]byte(`{"type":"mixed","tag":"without-principals"}`)); !errors.Is(err, failure) || owner.calls != 1 {
		t.Fatal("runtime error changed", err, owner.calls)
	}
	if err := adapter.RemoveInbound("exact-tag"); !errors.Is(err, failure) || owner.removed != "exact-tag" {
		t.Fatal("remove authority changed", err)
	}
	adapter.CloseInboundConnections("exact-tag")
	if owner.closed != "exact-tag" {
		t.Fatal("connection authority changed")
	}
}
