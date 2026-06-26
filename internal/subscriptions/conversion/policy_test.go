package conversion

import "testing"

func TestParsePolicyDefaultsAndLegacyFallback(t *testing.T) {
	policy := ParsePolicy("", ModeSelector)
	if got := policy.Mode(TargetOutbound, FeatureXrayBalancer); got != ModeSelector {
		t.Fatalf("outbound xray balancer mode = %q, want selector", got)
	}
	if got := policy.Mode(TargetOutbound, FeatureMihomoRelay); got != ModeSelector {
		t.Fatalf("relay default mode = %q, want selector", got)
	}
}

func TestParsePolicyKeepsOriginalOnlyForClientTargets(t *testing.T) {
	raw := `{
		"outbound":{"xrayBalancer":"original"},
		"client":{
			"xray":{"xrayBalancer":"original","mihomoFallback":"original"},
			"mihomo":{"xrayBalancer":"original","mihomoFallback":"original"}
		}
	}`
	policy := ParsePolicy(raw, "")
	if got := policy.Mode(TargetOutbound, FeatureXrayBalancer); got != ModeURLTest {
		t.Fatalf("outbound original mode = %q, want urltest fallback", got)
	}
	if got := policy.Mode(TargetXray, FeatureXrayBalancer); got != ModeOriginal {
		t.Fatalf("xray native original mode = %q, want original", got)
	}
	if got := policy.Mode(TargetXray, FeatureMihomoFallback); got != ModeXrayBalancer {
		t.Fatalf("xray mihomo original mode = %q, want balancer fallback", got)
	}
	if got := policy.Mode(TargetMihomo, FeatureXrayBalancer); got != ModeMihomoURLTest {
		t.Fatalf("mihomo xray original mode = %q, want url-test fallback", got)
	}
	if got := policy.Mode(TargetMihomo, FeatureMihomoFallback); got != ModeOriginal {
		t.Fatalf("mihomo native original mode = %q, want original", got)
	}
}

func TestParsePolicyMigratesLegacyRuntimeClientModes(t *testing.T) {
	raw := `{
		"client":{
			"xray":{"mihomoFallback":"urltest"},
			"mihomo":{"xrayBalancer":"failover"}
		}
	}`
	policy := ParsePolicy(raw, "")
	if got := policy.Mode(TargetXray, FeatureMihomoFallback); got != ModeXrayBalancer {
		t.Fatalf("legacy xray client mode = %q, want balancer", got)
	}
	if got := policy.Mode(TargetMihomo, FeatureXrayBalancer); got != ModeMihomoFallback {
		t.Fatalf("legacy mihomo client mode = %q, want fallback", got)
	}
}

func TestValidatePolicyJSONRejectsUnknownFeature(t *testing.T) {
	if err := ValidatePolicyJSON(`{"outbound":{"unknown":"urltest"}}`); err == nil {
		t.Fatal("expected unknown feature to be rejected")
	}
}

func TestValidatePolicyJSONAcceptsTargetSpecificModes(t *testing.T) {
	raw := `{
		"client":{
			"xray":{"mihomoFallback":"balancer"},
			"mihomo":{"xrayBalancer":"load-balance"}
		}
	}`
	if err := ValidatePolicyJSON(raw); err != nil {
		t.Fatalf("target-specific policy should be valid: %v", err)
	}
}

func TestValidatePolicyJSONRejectsOriginalForNonNativeClientTarget(t *testing.T) {
	for _, raw := range []string{
		`{"client":{"xray":{"mihomoFallback":"original"}}}`,
		`{"client":{"mihomo":{"xrayBalancer":"original"}}}`,
	} {
		if err := ValidatePolicyJSON(raw); err == nil {
			t.Fatalf("expected invalid original target to be rejected for %s", raw)
		}
	}
}
