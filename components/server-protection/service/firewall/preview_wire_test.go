package firewall

import (
	"encoding/json"
	"testing"
)

func TestFirewallPreviewEmptyCollectionsMarshalAsArrays(t *testing.T) {
	preview := Preview(FirewallPlan{}, PreviewOptions{})
	if preview.WouldKeep == nil || preview.WouldOpen == nil || preview.WouldWarn == nil || preview.WouldBlock == nil || preview.Warnings == nil || preview.ProtectedKeep == nil {
		t.Fatalf("preview contains nil collection: %#v", preview)
	}
	wire, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wouldKeep", "wouldOpen", "wouldWarn", "wouldBlock", "warnings", "protectedKeep"} {
		if len(fields[name]) == 0 || fields[name][0] != '[' {
			t.Errorf("%s=%s, want JSON array", name, fields[name])
		}
	}
	if string(fields["wouldOpen"]) != "[]" || string(fields["protectedKeep"]) != "[]" {
		t.Fatalf("empty preview collections were not []: wouldOpen=%s protectedKeep=%s", fields["wouldOpen"], fields["protectedKeep"])
	}
}
