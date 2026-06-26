package apply

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestRouterApply(t *testing.T) {
	var called bool
	router := NewRouter(map[Object]Handler{
		ObjectSettings: func(req MutationRequest, plan *Plan) error {
			called = true
			if req.Action != "set" {
				t.Fatalf("action = %q, want set", req.Action)
			}
			plan.IncludeSaveObjects(ObjectConfig)
			return nil
		},
	})

	plan := NewPlan(ObjectSettings.String())
	if err := router.Apply(MutationRequest{Object: "settings", Action: "set"}, &plan); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"settings", "config"}) {
		t.Fatalf("objects = %#v", got)
	}
}

func TestRouterRejectsUnknownObject(t *testing.T) {
	router := NewRouter(nil)
	plan := NewPlan("mystery")
	err := router.Apply(MutationRequest{Object: "mystery"}, &plan)
	if err == nil {
		t.Fatal("expected unknown object error")
	}
	if !strings.Contains(err.Error(), "unknown object: mystery") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"mystery"}) {
		t.Fatalf("unknown object should not mutate plan objects, got %#v", got)
	}
}

func TestRouterReportsHandlerObjects(t *testing.T) {
	router := NewRouter(map[Object]Handler{
		ObjectClients:  func(MutationRequest, *Plan) error { return nil },
		ObjectSettings: func(MutationRequest, *Plan) error { return nil },
	})
	got := router.HandlerObjectStrings()
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"clients", "settings"}) {
		t.Fatalf("handler objects = %#v", got)
	}
}
