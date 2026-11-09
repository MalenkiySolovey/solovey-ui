package hostintegration

import (
	"bytes"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-host/auditrecord"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/observation"
	"github.com/MalenkiySolovey/solovey-host/operationref"
	"github.com/MalenkiySolovey/solovey-host/secretref"
	"os"
	"strings"
	"testing"
)

func observedValues() map[string]any {
	access := secretref.Access{Reference: secretref.NewReference("telegram", "telegramBotToken"), Actor: secretref.Actor{Component: "telegram"}, Operation: secretref.UseForOwner, IntentID: "fixture-intent"}
	denied := access
	denied.Actor.Component = "foreign"
	invalidIntent := access
	invalidIntent.IntentID = "untrusted intent"
	return map[string]any{
		"local-host":                  hostref.Local(),
		"operation-reference":         operationref.NewReference("telegram", "secret:telegramBotToken", "fixture-intent"),
		"operation-correlation":       operationref.NewReference("telegram", "secret:telegramBotToken", "fixture-intent").Correlation(),
		"invalid-operation-reference": operationref.NewReference("", "invalid resource", ""),
		"secret-reference":            access.Reference,
		"masked-secret-view":          access.View(true, strings.Repeat("f", 64)),
		"nonrevision-secret-view":     access.View(true, "fake plaintext fixture"),
		"denied-owner-view":           denied.View(true, strings.Repeat("f", 64)),
		"denied-intent-view":          invalidIntent.View(true, strings.Repeat("f", 64)),
		"observability-sample":        observation.ObservabilitySample{DateTime: 1_800_000_000, CPU: 12.5, Memory: map[string]any{"total": 1024, "used": 256}, Network: map[string]any{"in": 100, "out": 50}},
		"core-sample":                 observation.CoreSample{DateTime: 1_800_000_000, Core: map[string]any{"connections": 3}},
		"metric-sample":               observation.ObservabilityMetricSample{DateTime: 1_800_000_000, Value: 12.5},
		"audit-event":                 auditrecord.Event{Actor: "component:telegram", Event: "fixture_result", Resource: "settings", Severity: "info", Details: map[string]any{"status": "ok"}},
	}
}

func TestNeutralValueCanonicalFixtures(t *testing.T) {
	data, err := os.ReadFile("../../host/fixtures/values.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	values := observedValues()
	if len(values) != 13 || len(fixtures) != len(values) {
		t.Fatal("unexpected value fixtures")
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			if err = json.Compact(&expected, fixtures[name]); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, expected.Bytes()) {
				t.Fatalf("neutral value differs from accepted JSON: %s", name)
			}
		})
	}
}
