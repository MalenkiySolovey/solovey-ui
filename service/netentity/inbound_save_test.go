package netentity

import (
	"reflect"
	"sort"
	"testing"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
)

func TestInboundSaveHandlersCoverSupportedActions(t *testing.T) {
	want := entityinbounds.SupportedActionStrings()
	got := make([]string, 0, len(inboundSaveHandlers))
	for action := range inboundSaveHandlers {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inbound save handlers = %#v, want %#v", got, want)
	}
}

func TestInboundSaveRejectsUnknownAction(t *testing.T) {
	if _, err := (&InboundService{}).applyInboundSave(inboundSaveRequest{action: "mystery"}); err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
}
