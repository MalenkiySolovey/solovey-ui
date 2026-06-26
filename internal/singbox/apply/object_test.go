package apply

import (
	"reflect"
	"testing"
)

func TestSupportedObjects(t *testing.T) {
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
	if got := SupportedObjectStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported objects = %#v, want %#v", got, want)
	}
}

func TestParseObject(t *testing.T) {
	object, ok := ParseObject("clients")
	if !ok {
		t.Fatal("expected clients object to be supported")
	}
	if object != ObjectClients {
		t.Fatalf("object = %q, want %q", object, ObjectClients)
	}
	if _, ok := ParseObject("mystery"); ok {
		t.Fatal("unexpected support for unknown object")
	}
}
