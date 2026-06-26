package apply

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type recordingMutationExecutor struct {
	calls       []string
	clientsIDs  []uint
	change      *Change
	configDirty bool
	fail        string
}

func (e *recordingMutationExecutor) record(call string) error {
	e.calls = append(e.calls, call)
	if e.fail == call {
		return errors.New(call + " failed")
	}
	return nil
}

func (e *recordingMutationExecutor) SaveClients(_ *gorm.DB, action string, _ json.RawMessage, hostname string) ([]uint, error) {
	if err := e.record("clients:" + action + ":" + hostname); err != nil {
		return nil, err
	}
	return e.clientsIDs, nil
}

func (e *recordingMutationExecutor) SaveTLS(_ *gorm.DB, action string, _ json.RawMessage, hostname string) error {
	return e.record("tls:" + action + ":" + hostname)
}

func (e *recordingMutationExecutor) SaveInbounds(_ *gorm.DB, action string, _ json.RawMessage, initUsers string, hostname string) (*Change, error) {
	if err := e.record("inbounds:" + action + ":" + initUsers + ":" + hostname); err != nil {
		return nil, err
	}
	return e.change, nil
}

func (e *recordingMutationExecutor) SaveOutbounds(_ *gorm.DB, action string, _ json.RawMessage) (*Change, error) {
	if err := e.record("outbounds:" + action); err != nil {
		return nil, err
	}
	return e.change, nil
}

func (e *recordingMutationExecutor) SaveServices(_ *gorm.DB, action string, _ json.RawMessage) (*Change, error) {
	if err := e.record("services:" + action); err != nil {
		return nil, err
	}
	return e.change, nil
}

func (e *recordingMutationExecutor) SaveEndpoints(_ *gorm.DB, action string, _ json.RawMessage) (*Change, error) {
	if err := e.record("endpoints:" + action); err != nil {
		return nil, err
	}
	return e.change, nil
}

func (e *recordingMutationExecutor) ConfigBlobChanged(_ *gorm.DB, _ json.RawMessage) (bool, error) {
	if err := e.record("config:changed"); err != nil {
		return false, err
	}
	return e.configDirty, nil
}

func (e *recordingMutationExecutor) SaveBaseConfig(_ *gorm.DB, _ json.RawMessage) error {
	return e.record("config:save")
}

func (e *recordingMutationExecutor) SaveSettings(_ *gorm.DB, _ json.RawMessage) error {
	return e.record("settings:save")
}

func TestMutationHandlerObjectsCoverSupportedObjects(t *testing.T) {
	want := SupportedObjectStrings()
	got := MutationHandlerObjectStrings()
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutation handler objects = %#v, want %#v", got, want)
	}
}

func TestApplyMutationClientsCascadesInboundReload(t *testing.T) {
	executor := &recordingMutationExecutor{clientsIDs: []uint{7}}
	plan := NewPlan(ObjectClients.String())
	err := ApplyMutation(executor, MutationRequest{Object: "clients", Action: "edit", Hostname: "example.com"}, &plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(executor.calls, []string{"clients:edit:example.com"}) {
		t.Fatalf("calls = %#v", executor.calls)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"clients", "inbounds"}) {
		t.Fatalf("objects = %#v", got)
	}
	if got := plan.InboundIDs(); !reflect.DeepEqual(got, []uint{7}) {
		t.Fatalf("inbound ids = %#v", got)
	}
}

func TestApplyMutationTLSCascadesRuntimeRestart(t *testing.T) {
	executor := &recordingMutationExecutor{}
	plan := NewPlan(ObjectTLS.String())
	if err := ApplyMutation(executor, MutationRequest{Object: "tls", Action: "edit", Hostname: "example.com"}, &plan); err != nil {
		t.Fatal(err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"tls", "clients", "inbounds"}) {
		t.Fatalf("objects = %#v", got)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("TLS changes should require core restart")
	}
}

func TestApplyMutationBaseConfigValidatesLogOutputAndRestartsWhenDirty(t *testing.T) {
	executor := &recordingMutationExecutor{configDirty: true}
	plan := NewPlan(ObjectConfig.String())
	if err := ApplyMutation(executor, MutationRequest{Object: "config", Data: json.RawMessage(`{"log":{}}`)}, &plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(executor.calls, []string{"config:changed", "config:save"}) {
		t.Fatalf("calls = %#v", executor.calls)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("changed base config should require core restart")
	}
}

func TestApplyMutationBaseConfigRejectsUnsafeLogOutput(t *testing.T) {
	executor := &recordingMutationExecutor{}
	plan := NewPlan(ObjectConfig.String())
	err := ApplyMutation(executor, MutationRequest{Object: "config", Data: json.RawMessage(`{"log":{"output":"../../etc/passwd"}}`)}, &plan)
	if err == nil {
		t.Fatal("expected unsafe log output to be rejected")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor should not be called after validation error, got %#v", executor.calls)
	}
}

func TestApplyMutationRejectsMissingExecutor(t *testing.T) {
	plan := NewPlan(ObjectSettings.String())
	err := ApplyMutation(nil, MutationRequest{Object: "settings"}, &plan)
	if err == nil || !strings.Contains(err.Error(), "missing config save executor") {
		t.Fatalf("unexpected error: %v", err)
	}
}
