package service

import (
	"reflect"
	"strings"
	"testing"

	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
)

func TestParseConfigSaveObject(t *testing.T) {
	object, ok := singboxapply.ParseObject("clients")
	if !ok {
		t.Fatal("expected clients object to be supported")
	}
	if object != singboxapply.ObjectClients {
		t.Fatalf("parsed object = %q, want %q", object, singboxapply.ObjectClients)
	}
	if _, ok := singboxapply.ParseObject("mystery"); ok {
		t.Fatal("unexpected support for unknown save object")
	}
}

func TestApplyConfigSaveObjectRejectsUnknownObject(t *testing.T) {
	plan := newConfigSavePlan("mystery")
	err := applyConfigSaveObject(&ConfigService{}, singboxapply.MutationRequest{Object: "mystery"}, &plan.Plan)
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
