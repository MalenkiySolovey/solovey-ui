package netentity

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCoreOwnerBlocksInboundAndTLSMutationWhileFrontingLeaseExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil || db.AutoMigrate(&model.Inbound{}, &model.Tls{}, &model.InboundEndpointLease{}) != nil {
		t.Fatalf("open/migrate authority DB: %v", err)
	}
	if err := db.Create(&model.Inbound{Id: 7, Tag: "leased", Type: "trojan", TlsId: 9, Options: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundEndpointLease{LeaseID: "lease-1", InboundID: 7, ProviderID: "core", HolderID: "operation-1",
		ResourceID: "core:inbound:7", EndpointID: "endpoint-1", ExactReferenceJSON: []byte(`{}`), LeaseJSON: []byte(`{}`),
		LeaseRevision: strings.Repeat("a", 64), State: "ACTIVE", LastRequestID: "request-1", IssuedAtUnix: 1,
		RenewedAtUnix: 1, ExpiresAtUnix: 100}).Error; err != nil {
		t.Fatal(err)
	}
	service := &InboundService{}
	if _, err := service.applyInboundSave(inboundSaveRequest{tx: db, action: "edit", data: json.RawMessage(`{"id":7}`)}); !errors.Is(err, ErrFrontingEndpointLeased) {
		t.Fatalf("leased inbound edit error=%v", err)
	}
	if _, err := service.applyInboundSave(inboundSaveRequest{tx: db, action: "del", data: json.RawMessage(`"leased"`)}); !errors.Is(err, ErrFrontingEndpointLeased) {
		t.Fatalf("leased inbound delete error=%v", err)
	}
	if err := (&TlsService{}).applyTLSSave(tlsSaveRequest{tx: db, action: "edit", data: json.RawMessage(`{"id":9}`)}); !errors.Is(err, ErrFrontingEndpointLeased) {
		t.Fatalf("leased TLS edit error=%v", err)
	}
	if err := db.Model(&model.InboundEndpointLease{}).Where("lease_id = ?", "lease-1").Update("state", "RELEASED").Error; err != nil {
		t.Fatal(err)
	}
	if err := guardInboundFrontingLease(db, "edit", json.RawMessage(`{"id":7}`)); err != nil {
		t.Fatalf("released authority still blocked inbound edit: %v", err)
	}
	if err := guardTLSFrontingLease(db, "edit", json.RawMessage(`{"id":9}`)); err != nil {
		t.Fatalf("released authority still blocked TLS edit: %v", err)
	}
}
