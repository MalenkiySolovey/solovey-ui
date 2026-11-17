package validation

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/backend/settings/catalog"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/backend/settings/schema"
	"testing"
)

func TestFieldMetadataMatchesAuthoritativeDynamicValidation(t *testing.T) {
	defaults := settingcatalog.MergeDefaultMaps(
		settingcatalog.WebDefaults(),
		settingcatalog.SessionDefaults("secret", "salt"),
		settingcatalog.RuntimeDefaults(),
		settingcatalog.SubscriptionDefaults(),
		settingcatalog.IPCertDefaults(),
		settingcatalog.InternalDefaults("{}"),
	)
	schema := settingsschema.New(defaults, nil, nil, settingsschema.DefaultFieldMetadata()...)

	applyTarget, ok := schema.Field(settingcatalog.IPCertApplyTargetKey)
	if !ok {
		t.Fatal("ipCertApplyTarget field is missing")
	}
	if applyTarget.Type != settingsschema.FieldTypeString || len(applyTarget.Options) != 0 {
		t.Fatalf("dynamic apply target exposed as a fixed enum: %#v", applyTarget)
	}
	for _, value := range []string{"panel", "inbound:42"} {
		if err := ValidateIPCertApplyTarget(value); err != nil {
			t.Fatalf("schema-compatible apply target %q rejected: %v", value, err)
		}
	}

	rateLimit, ok := schema.Field(settingcatalog.SubRateLimitPerIPKey)
	if !ok || rateLimit.Min == nil || *rateLimit.Min != 1 || rateLimit.Max == nil || *rateLimit.Max != 10000 {
		t.Fatalf("subscription rate-limit bounds do not match validation: %#v", rateLimit)
	}
}
