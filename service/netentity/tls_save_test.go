package netentity

import (
	"reflect"
	"sort"
	"testing"

	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
)

func TestTLSSaveHandlersCoverSupportedActions(t *testing.T) {
	want := entitytls.SupportedActionStrings()
	got := make([]string, 0, len(tlsSaveHandlers))
	for action := range tlsSaveHandlers {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TLS save handlers = %#v, want %#v", got, want)
	}
}

func TestTLSSaveKeepsUnknownActionNoop(t *testing.T) {
	if err := (&TlsService{}).applyTLSSave(tlsSaveRequest{action: "mystery"}); err != nil {
		t.Fatalf("unknown TLS action should remain a no-op, got %v", err)
	}
}
