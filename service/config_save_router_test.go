package service

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestConfigSaveHandlersCoverSupportedObjects(t *testing.T) {
	want := []string{
		"clients",
		"config",
		"endpoints",
		"inbounds",
		"outbounds",
		"services",
		"settings",
		"tls",
	}
	if got := supportedConfigSaveObjectStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported config save objects = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(configSaveHandlers))
	for object := range configSaveHandlers {
		got = append(got, object.String())
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config save handlers = %#v, want %#v", got, want)
	}
}

func TestParseConfigSaveObject(t *testing.T) {
	object, ok := parseConfigSaveObject("clients")
	if !ok {
		t.Fatal("expected clients object to be supported")
	}
	if object != configSaveObjectClients {
		t.Fatalf("parsed object = %q, want %q", object, configSaveObjectClients)
	}
	if _, ok := parseConfigSaveObject("mystery"); ok {
		t.Fatal("unexpected support for unknown save object")
	}
}

func TestApplyConfigSaveObjectRejectsUnknownObject(t *testing.T) {
	plan := newConfigSavePlan("mystery")
	err := applyConfigSaveObject(&ConfigService{}, configSaveRequest{object: "mystery"}, &plan)
	if err == nil {
		t.Fatal("expected unknown config save object to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown object: mystery") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"mystery"}) {
		t.Fatalf("unknown object should not mutate plan objects, got %#v", got)
	}
	if plan.RequiresCoreRestart() {
		t.Fatal("unknown object should not require core restart")
	}
}
