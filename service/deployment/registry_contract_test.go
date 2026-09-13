package deployment

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEveryProductionDeploymentRegistrationRequiresItsExplicitKind(t *testing.T) {
	kinds := make([]string, 0, len(runtimeProviderFactories))
	for kind := range runtimeProviderFactories {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	if len(kinds) < 3 {
		t.Fatalf("production deployment registry is unexpectedly incomplete: %v", kinds)
	}
	for _, kind := range kinds {
		factory := runtimeProviderFactories[kind]
		if factory == nil {
			t.Fatalf("production deployment registration %q has no factory", kind)
		}
		direct := factory()
		if direct == nil || direct.ProviderID() == "" {
			t.Fatalf("production deployment registration %q returned no semantic provider", kind)
		}
		for _, input := range []string{kind, "  " + strings.ToUpper(kind) + "  "} {
			t.Run(kind+"/"+strings.TrimSpace(input), func(t *testing.T) {
				t.Setenv("SUI_DEPLOYMENT_KIND", input)
				selected := RuntimeProvider()
				if reflect.TypeOf(selected) != reflect.TypeOf(direct) || selected.ProviderID() != direct.ProviderID() {
					t.Fatalf("explicit kind %q selected %T/%q, want %T/%q", input, selected, selected.ProviderID(), direct, direct.ProviderID())
				}
				// Capability projection is executed through the selected production
				// provider so the test cannot pass on registry lookup alone.
				if revision := selected.Capabilities(context.Background()).Revision; revision == "" {
					t.Fatalf("selected provider %q returned an unrevisioned capability projection", kind)
				}
			})
		}
	}

	for _, input := range []string{"", "not-registered", "systemd-probably", "openwrt-detected"} {
		t.Run("unavailable/"+input, func(t *testing.T) {
			t.Setenv("SUI_DEPLOYMENT_KIND", input)
			if _, ok := RuntimeProvider().(UnavailableProvider); !ok {
				t.Fatalf("absent or unknown explicit kind %q selected %T", input, RuntimeProvider())
			}
		})
	}
}
