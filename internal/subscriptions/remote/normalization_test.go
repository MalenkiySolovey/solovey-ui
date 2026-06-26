package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestSanitizeTagPrefixKeepsReadableUnicode(t *testing.T) {
	got := SanitizeTagPrefix("🇳🇱Авто | (20 Гбит/c BURST-WS🌀)")
	if !strings.Contains(got, "Авто") {
		t.Fatalf("unicode text should stay readable, got %q", got)
	}
	if !strings.HasSuffix(got, "-") {
		t.Fatalf("prefix should end with separator, got %q", got)
	}
}

func TestDefaultTagPrefixUsesIDPlaceholder(t *testing.T) {
	got := DefaultTagPrefix("Auto Balance", 0)
	if got != "ros{id}-Auto-Balance-" {
		t.Fatalf("default prefix = %q", got)
	}
}

func TestNormalizeOutboundUsesLabelSourceKey(t *testing.T) {
	normalized, err := NormalizeOutbound(map[string]any{
		"type":        "vless",
		"tag":         "Node A",
		"server":      "example.com",
		"server_port": 443,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != "Node A" {
		t.Fatalf("name = %q", normalized.Name)
	}
	if normalized.SourceKey != "label:nodea" {
		t.Fatalf("source key = %q", normalized.SourceKey)
	}
	var options map[string]any
	if err := json.Unmarshal(normalized.Options, &options); err != nil {
		t.Fatal(err)
	}
	if _, exists := options["tag"]; exists {
		t.Fatalf("tag should be removed from options: %s", normalized.Options)
	}
}

func TestNormalizeOutboundStoresAdaptationButStripsRuntimeMetadata(t *testing.T) {
	normalized, err := NormalizeOutbound(map[string]any{
		"type":      "urltest",
		"tag":       "Auto",
		"outbounds": []string{"Node A"},
		subcanonical.MetadataKey: map[string]any{
			"source_format":  subcanonical.FormatXray,
			"source_feature": "routing.balancer",
			"source_type":    "balancer",
			"target_type":    "urltest",
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(normalized.Options, &options); err != nil {
		t.Fatal(err)
	}
	if _, exists := options[subcanonical.MetadataKey]; exists {
		t.Fatalf("options leaked metadata: %s", normalized.Options)
	}
	var connection subcanonical.Connection
	if err := json.Unmarshal(normalized.Canonical, &connection); err != nil {
		t.Fatal(err)
	}
	if len(connection.Adaptations) != 1 || connection.Adaptations[0].SourceFeature != "routing.balancer" {
		t.Fatalf("canonical adaptation = %#v", connection.Adaptations)
	}
}

func TestUpdateConnectionComparesJSONSemantically(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Name:    "Node",
		Type:    "vless",
		Missing: true,
		Options: json.RawMessage(`{"a":1,"b":2}`),
	}
	changed := UpdateConnection(&connection, NormalizedOutbound{
		Name:    "Node",
		Type:    "vless",
		Options: json.RawMessage(`{"b":2,"a":1}`),
	}, 123)
	if !changed {
		t.Fatal("missing -> present should be reported as changed")
	}
	if connection.Missing {
		t.Fatal("missing flag should be cleared")
	}
	if string(connection.Options) != `{"a":1,"b":2}` {
		t.Fatalf("semantically equal options should not be rewritten, got %s", connection.Options)
	}
	if connection.LastSeen != 123 || connection.UpdatedAt != 123 {
		t.Fatalf("timestamps were not updated: %#v", connection)
	}
}

func TestUniqueSourceKey(t *testing.T) {
	seen := map[string]struct{}{"a": {}, "a:2": {}}
	if got := UniqueSourceKey(seen, "a"); got != "a:3" {
		t.Fatalf("unique source key = %q", got)
	}
}
