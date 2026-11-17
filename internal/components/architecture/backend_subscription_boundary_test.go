package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestSubscriptionPublisherExcludesProductCompositionAndProviderRuntime(t *testing.T) {
	root := moduleRoot(t)
	var violations []string
	for name := range goListDependencies(t, root, backendModulePath+"/subscriptions") {
		privateProduct := strings.HasPrefix(name, modulePath+"/") && !strings.HasPrefix(name, modulePath+"/backend/")
		providerRuntime := strings.HasPrefix(name, "github.com/sagernet/sing-box") || strings.HasPrefix(name, "github.com/MalenkiySolovey/sing-box")
		if privateProduct || providerRuntime {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("subscription publishing imports Product composition or provider runtime: %s", strings.Join(violations, ", "))
	}
}
