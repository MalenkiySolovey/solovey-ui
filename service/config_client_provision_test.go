package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
)

func TestProvisionClientUsesPanelClientOwner(t *testing.T) {
	initSettingTestDB(t)
	s := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	ctx, release, err := dbsqlite.AcquireOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	request := ClientProvision{
		Name: "tg41", Config: json.RawMessage(`{"mixed":{"username":"tg41","password":"fixture-password"}}`),
		Inbounds: []uint{}, Volume: 1024, Expiry: 2_000_000_000, Description: "trial fixture",
	}
	const actor = "PaidSubBot"
	const hostname = "fixture.example"
	id, err := s.ProvisionClient(ctx, request, actor, hostname)
	if err != nil || id == 0 {
		t.Fatalf("provisioned client id=%d, err=%v", id, err)
	}
	duplicate := request
	duplicate.Volume++
	if _, err := s.ProvisionClient(ctx, duplicate, actor, hostname); err == nil {
		t.Fatal("create command accepted a duplicate client name")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	request.Name = "tg42"
	if _, err := s.ProvisionClient(canceled, request, actor, hostname); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provisioning err=%v, want context.Canceled", err)
	}
	var clients []model.Client
	if err := dbsqlite.DB().Find(&clients).Error; err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].Id != id || clients[0].Name != "tg41" || !clients[0].Enable || clients[0].Volume != request.Volume || clients[0].Expiry != request.Expiry || clients[0].Desc != request.Description {
		t.Fatal("only the original enabled client and its limits should be committed")
	}
	changes := s.GetChanges(actor, "clients", "10")
	if len(changes) != 1 || changes[0].Action != "new" || strings.Contains(string(changes[0].Obj), "fixture-password") {
		t.Fatal("expected one redacted canonical client creation record")
	}
}
