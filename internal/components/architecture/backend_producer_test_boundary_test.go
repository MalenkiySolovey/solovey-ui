package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendProducerClosureIncludesPortableTests(t *testing.T) {
	var rejected []string
	for name := range goListDependencies(t, moduleRoot(t), "./backend/...", "-test") {
		if (name == modulePath || strings.HasPrefix(name, modulePath+"/")) && !strings.HasPrefix(name, modulePath+"/backend/") {
			rejected = append(rejected, name)
		}
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		t.Fatalf("Backend production or test closure imports Product-private owners: %s", strings.Join(rejected, ", "))
	}
}
