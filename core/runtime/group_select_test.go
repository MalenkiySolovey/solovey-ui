package runtime

import (
	"errors"
	"testing"
)

func TestGroupSelect(t *testing.T) {
	core := NewCore()
	config := []byte(`{
		"log":{"disabled":true},
		"outbounds":[
			{"type":"direct","tag":"direct"},
			{"type":"socks","tag":"proxy","server":"127.0.0.1","server_port":1080},
			{"type":"selector","tag":"group","outbounds":["proxy","direct"],"default":"proxy"}
		]
	}`)
	if err := core.Start(config); err != nil {
		t.Skipf("minimal core start unavailable: %v", err)
	}
	t.Cleanup(func() { _ = core.Stop() })

	if active, ok := core.GroupNow("group"); !ok || active != "proxy" {
		t.Fatalf("initial active = %q,%v", active, ok)
	}
	if err := core.SelectGroupMember("group", "direct"); err != nil {
		t.Fatal(err)
	}
	if active, _ := core.GroupNow("group"); active != "direct" {
		t.Fatalf("active after switch = %q", active)
	}
	if err := core.SelectGroupMember("group", "missing"); !errors.Is(err, ErrMemberNotInGroup) {
		t.Fatalf("missing member error = %v", err)
	}
	if err := core.SelectGroupMember("direct", "x"); !errors.Is(err, ErrNotSelectorGroup) {
		t.Fatalf("non-selector error = %v", err)
	}
	if err := core.SelectGroupMember("missing", "x"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("missing group error = %v", err)
	}
}
