package architecture

import (
	"strings"
	"testing"
)

func TestSubscriptionServerExcludesProductAndProviderImplementation(t *testing.T) {
	for name := range goListDependencies(t, moduleRoot(t), "./backend/subscriptions/server") {
		if strings.HasPrefix(name, modulePath+"/") && !strings.HasPrefix(name, modulePath+"/backend/") {
			t.Errorf("subscription server imports Product implementation: %s", name)
		}
		if strings.HasPrefix(name, "github.com/MalenkiySolovey/solovey-node/") || strings.Contains(name, "sing-box") {
			t.Errorf("subscription server imports a provider or remote Node: %s", name)
		}
	}
}
