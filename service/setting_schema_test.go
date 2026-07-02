package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFrontendSettingsPayloadDefaultsMatchBackendSchema(t *testing.T) {
	registerSettingsPayloadContributionsForTest(t)
	payloadObjects := []struct {
		file string
		name string
	}{
		{
			file: filepath.Join("..", "frontend", "src", "views", "settingsPayload.ts"),
			name: "settingsPageDefaults",
		},
		{
			file: filepath.Join("..", "components", "telegram", "frontend", "views", "telegramSettingsPayload.ts"),
			name: "telegramSettingsDefaults",
		},
		{
			file: filepath.Join("..", "components", "paid-subscriptions", "frontend", "views", "paidSubSettingsPayload.ts"),
			name: "paidSubSettingsDefaults",
		},
	}

	for _, payloadObject := range payloadObjects {
		raw, err := os.ReadFile(payloadObject.file)
		if err != nil {
			t.Fatalf("read frontend settings payload %s: %v", payloadObject.file, err)
		}
		schema := currentSettingsSchema()
		defaults := parseSettingsPayloadDefaults(t, string(raw), payloadObject.name)
		if len(defaults) == 0 {
			t.Fatalf("%s has no parsed defaults", payloadObject.name)
		}
		for key, value := range defaults {
			if strings.HasSuffix(key, "HasSecret") {
				if value != "false" {
					t.Fatalf("%s marker %s default = %q, want false", payloadObject.name, key, value)
				}
				if !schema.AcceptsSecretPresenceMarker(key) {
					t.Fatalf("%s marker %s is not accepted by backend schema", payloadObject.name, key)
				}
				continue
			}
			backendDefault, ok := schema.Default(key)
			if !ok {
				t.Fatalf("%s key %s does not exist in backend schema", payloadObject.name, key)
			}
			if !schema.Editable(key) {
				t.Fatalf("%s key %s is not editable in backend schema", payloadObject.name, key)
			}
			if backendDefault != value {
				t.Fatalf("%s key %s frontend default = %q, backend default = %q", payloadObject.name, key, value, backendDefault)
			}
		}
	}
}

func TestPublicSettingSchemaHasUsableMetadata(t *testing.T) {
	fields := (&SettingService{}).GetSettingSchema()
	if len(fields) == 0 {
		t.Fatal("public setting schema is empty")
	}
	seen := map[string]struct{}{}
	for _, field := range fields {
		if field.Key == "" || field.Page == "" || field.Group == "" || field.LabelKey == "" || field.Type == "" {
			t.Fatalf("incomplete field metadata: %#v", field)
		}
		if field.Internal || !field.Editable {
			t.Fatalf("public schema leaked non-editable/internal field: %#v", field)
		}
		if field.Encrypted && field.SecretPresenceKey == "" {
			t.Fatalf("encrypted field has no secret marker: %#v", field)
		}
		if _, ok := seen[field.Key]; ok {
			t.Fatalf("duplicate field in public schema: %s", field.Key)
		}
		seen[field.Key] = struct{}{}
	}
}

func parseSettingsPayloadDefaults(t *testing.T, raw string, objectName string) map[string]string {
	t.Helper()
	objectRE := regexp.MustCompile(`(?s)export const ` + regexp.QuoteMeta(objectName) + `: SettingsMap = \{(.*?)\n\}`)
	match := objectRE.FindStringSubmatch(raw)
	if len(match) != 2 {
		t.Fatalf("could not find %s object", objectName)
	}
	pairRE := regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+): '([^']*)',`)
	defaults := map[string]string{}
	for _, pair := range pairRE.FindAllStringSubmatch(match[1], -1) {
		defaults[pair[1]] = pair[2]
	}
	return defaults
}
