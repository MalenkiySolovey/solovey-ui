package users

import (
	"reflect"
	"sort"
	"testing"
)

func TestSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "addbulk", "editbulk", "delbulk", "del"}
	if got := SupportedActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported client save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client save actions = %#v, want %#v", got, want)
	}
}

func TestParseAction(t *testing.T) {
	action, ok := ParseAction("editbulk")
	if !ok {
		t.Fatal("expected editbulk action to be supported")
	}
	if action != ActionEditBulk {
		t.Fatalf("parsed action = %q, want %q", action, ActionEditBulk)
	}
	if _, ok := ParseAction("mystery"); ok {
		t.Fatal("unexpected support for unknown client save action")
	}
}
