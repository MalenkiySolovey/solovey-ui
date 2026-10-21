package componenthost

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

type portAudit struct{}

func (portAudit) Record(service.AuditEvent) error { return nil }

func TestBindingPopulatesOnlyDeclaredPortsAndActualHost(t *testing.T) {
	host := APIDeps{
		OutboundHTTP: OutboundHTTPClientFunc(func(string, time.Duration) (*http.Client, error) { return nil, nil }),
		Audit:        AuditDeps{Recorder: portAudit{}},
		Auth:         AuthDeps{RequireScope: func(*gin.Context, string, ...string) bool { return true }},
		HTTP:         HTTPDeps{JSONObj: func(*gin.Context, interface{}, error) {}},
	}
	contract := manifest.HostAPI{Version: manifest.HostAPIV1, API: []string{manifest.PortAuditRecord}}
	ports, err := host.BindAPI(contract)
	if err != nil || !ports.Host.IsLocal() || ports.Audit.Recorder == nil || ports.OutboundHTTP != nil || ports.Auth.RequireScope != nil || ports.HTTP.JSONObj != nil {
		t.Fatalf("undeclared authority or incorrect host: %+v %v", ports, err)
	}
	contract.API = []string{manifest.PortHTTPRequest, manifest.PortOutboundHTTP}
	ports, err = host.BindAPI(contract)
	if err != nil || ports.OutboundHTTP == nil || ports.Auth.RequireScope == nil || ports.Audit.Recorder != nil {
		t.Fatalf("request binding: %+v %v", ports, err)
	}
	for _, name := range []string{"Runtime", "DB", "Database", "Exec", "Filesystem"} {
		if _, exists := reflect.TypeFor[Ports]().FieldByName(name); exists {
			t.Fatalf("typed component boundary exposes %s", name)
		}
	}
}

func TestDataDropPortIsBoundToDeclaredOwnerWithoutCallerControlledKeys(t *testing.T) {
	item := manifest.Manifest{ID: "typed-settings-owner", Name: "Typed settings owner", Delivery: manifest.DeliveryInProcess,
		HostAPI:  &manifest.HostAPI{Version: manifest.HostAPIV1, DropData: []string{manifest.PortSettingsDrop}},
		Database: manifest.Database{Settings: []string{"typed.fixture"}}}
	if ports, err := (Deps{}).BindDataDrop(item); err == nil || !reflect.DeepEqual(ports, Ports{}) {
		t.Fatal("missing data port admitted")
	}
	var actualOwner string
	ports, err := (Deps{DropOwnedSettings: func(_ context.Context, owner string) error { actualOwner = owner; return nil }}).BindDataDrop(item)
	if err != nil || ports.DropOwnedSettings == nil {
		t.Fatal(err)
	}
	if err := ports.DropOwnedSettings(t.Context()); err != nil || actualOwner != item.ID {
		t.Fatal("data-drop scope drift", err)
	}
	api, err := (APIDeps{}).BindAPI(*item.HostAPI)
	if err != nil || api.DropOwnedSettings != nil {
		t.Fatal("API received lifecycle deletion authority", err)
	}
}

func TestBindingFailsBeforeReturningPartialAuthority(t *testing.T) {
	for _, contract := range []manifest.HostAPI{
		{Version: "future"},
		{Version: manifest.HostAPIV1, Lifecycle: []string{"runtime"}},
		{Version: manifest.HostAPIV1, Lifecycle: []string{manifest.PortAuditRecord, manifest.PortOutboundHTTP}},
		{Version: manifest.HostAPIV1, Lifecycle: []string{manifest.PortScheduler}},
		{Version: manifest.HostAPIV1, API: []string{manifest.PortHTTPRequest}},
	} {
		host := Deps{API: APIDeps{Audit: AuditDeps{Recorder: portAudit{}}}}
		ports, err := host.BindLifecycle(contract)
		if len(contract.API) != 0 {
			ports, err = host.API.BindAPI(contract)
		}
		if err == nil || !reflect.DeepEqual(ports, Ports{}) {
			t.Fatalf("failed binding returned authority: %+v %v", ports, err)
		}
	}
}
