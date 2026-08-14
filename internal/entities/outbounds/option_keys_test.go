package outbounds

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestDecodeSaveOutboundStripsRegisteredPresentationKeys(t *testing.T) {
	unregister := RegisterOptionStripKeys("test-component", "componentManaged")
	t.Cleanup(unregister)

	outbound, err := decodeSaveOutbound(json.RawMessage(`{"type":"direct","tag":"direct","componentManaged":true,"server":"example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	if _, ok := options["componentManaged"]; ok {
		t.Fatalf("registered presentation key leaked into options: %s", outbound.Options)
	}
	if options["server"] != "example.com" {
		t.Fatalf("ordinary outbound option was removed: %s", outbound.Options)
	}
}

func TestPersistenceUnmarshalDoesNotDependOnOptionRegistry(t *testing.T) {
	unregister := RegisterOptionStripKeys("persistence-boundary-test", "componentManaged")
	t.Cleanup(unregister)

	var outbound model.Outbound
	if err := json.Unmarshal([]byte(`{"type":"direct","tag":"direct","componentManaged":true}`), &outbound); err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	if _, ok := options["componentManaged"]; !ok {
		t.Fatal("persistence unmarshal unexpectedly consulted the domain registry")
	}
}
