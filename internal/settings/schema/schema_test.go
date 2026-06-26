package schema

import (
	"reflect"
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestSchemaCopiesInputAndExposesKeyProperties(t *testing.T) {
	defaults := map[string]string{"shown": "1", "hidden": "2", "secret": "3"}
	internal := map[string]struct{}{"hidden": {}}
	encrypted := map[string]struct{}{"secret": {}}
	schema := New(defaults, internal, encrypted)

	defaults["later"] = "4"
	internal["shown"] = struct{}{}
	encrypted["shown"] = struct{}{}

	if got := schema.Keys(); !reflect.DeepEqual(got, []string{"hidden", "secret", "shown"}) {
		t.Fatalf("keys = %#v", got)
	}
	if value, ok := schema.Default("shown"); !ok || value != "1" {
		t.Fatalf("default shown = %q, %v", value, ok)
	}
	if schema.Editable("hidden") {
		t.Fatal("internal key should not be editable")
	}
	if !schema.Editable("shown") {
		t.Fatal("shown key should stay editable")
	}
	if !schema.Encrypted("secret") {
		t.Fatal("secret key should be encrypted")
	}
	if schema.Encrypted("shown") {
		t.Fatal("schema should copy encrypted keys")
	}
}

func TestSchemaHideInternal(t *testing.T) {
	schema := New(map[string]string{"shown": "1", "hidden": "2"}, map[string]struct{}{"hidden": {}}, nil)
	values := map[string]string{"shown": "1", "hidden": "2"}
	schema.HideInternal(values)

	if _, ok := values["hidden"]; ok {
		t.Fatalf("internal key was not hidden: %#v", values)
	}
	if values["shown"] != "1" {
		t.Fatalf("visible key changed: %#v", values)
	}
}

func TestSecretPresenceMarkers(t *testing.T) {
	schema := New(map[string]string{"token": ""}, nil, map[string]struct{}{"token": {}})

	if SecretPresenceKey("token") != "tokenHasSecret" {
		t.Fatal("unexpected secret presence key")
	}
	if !IsSecretPresenceMarker("tokenHasSecret") {
		t.Fatal("expected HasSecret marker")
	}
	if BaseSecretKey("tokenHasSecret") != "token" {
		t.Fatal("unexpected base secret key")
	}
	if !schema.AcceptsSecretPresenceMarker("tokenHasSecret") {
		t.Fatal("encrypted key marker should be accepted")
	}
	if schema.AcceptsSecretPresenceMarker("otherHasSecret") {
		t.Fatal("unknown marker should not be accepted")
	}
	if schema.AcceptsSecretPresenceMarker("token") {
		t.Fatal("plain key is not a marker")
	}
}

func TestSchemaFieldsCombineMetadataWithKeyProperties(t *testing.T) {
	schema := New(
		map[string]string{"shown": "1", "hidden": "2", "secret": ""},
		map[string]struct{}{"hidden": {}},
		map[string]struct{}{"secret": {}},
		Field{Key: "shown", Page: PageSettings, Group: GroupRuntime, Type: FieldTypeInt, LabelKey: "shown.label", Min: intPtr(0), Order: 20},
		Field{Key: "secret", Page: PageTelegram, Group: GroupTelegramCore, LabelKey: "secret.label", Order: 10},
	)

	shown, ok := schema.Field("shown")
	if !ok {
		t.Fatal("shown field missing")
	}
	if shown.Default != "1" || shown.Internal || !shown.Editable || shown.Type != FieldTypeInt || shown.LabelKey != "shown.label" {
		t.Fatalf("unexpected shown field: %#v", shown)
	}
	if shown.Min == nil || *shown.Min != 0 {
		t.Fatalf("min was not copied into field metadata: %#v", shown)
	}

	secret, ok := schema.Field("secret")
	if !ok {
		t.Fatal("secret field missing")
	}
	if !secret.Encrypted || secret.Type != FieldTypeSecret || secret.SecretPresenceKey != "secretHasSecret" {
		t.Fatalf("unexpected secret field: %#v", secret)
	}

	hidden, ok := schema.Field("hidden")
	if !ok {
		t.Fatal("hidden field missing")
	}
	if !hidden.Internal || hidden.Editable || hidden.Page != PageInternal {
		t.Fatalf("unexpected hidden field: %#v", hidden)
	}
}

func TestDefaultFieldMetadataCoversEveryKnownDefault(t *testing.T) {
	fields := fieldsByKey(DefaultFieldMetadata())
	defaults := map[string]string{}
	for _, group := range []map[string]string{
		settingcatalog.WebDefaults(),
		settingcatalog.SessionDefaults("secret", "salt"),
		settingcatalog.RuntimeDefaults(),
		settingcatalog.InternalDefaults("{}"),
		settingcatalog.SubscriptionDefaults(),
		settingcatalog.TelegramDefaults(),
		settingcatalog.PaidSubDefaults(),
		settingcatalog.IPCertDefaults(),
	} {
		for key, value := range group {
			defaults[key] = value
		}
	}
	for key := range defaults {
		field, ok := fields[key]
		if !ok {
			t.Fatalf("default settings key %q has no field metadata", key)
		}
		if field.Type == "" || field.Page == "" || field.Group == "" || field.LabelKey == "" {
			t.Fatalf("field metadata for %q is incomplete: %#v", key, field)
		}
	}
}
