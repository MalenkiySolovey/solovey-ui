package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendOperationCoordinationPreservesNarrowDurableOwners(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{
		backendModulePath + "/operations/coordination": true,
	}
	for _, target := range []string{
		"github.com/MalenkiySolovey/solovey-host/operationref",
		backendModulePath + "/storage/model",
		backendModulePath + "/storage/sqlite",
		"gorm.io/gorm",
	} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/operations/coordination") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Backend operation coordination imports Product execution or another authority: %s", strings.Join(violations, ", "))
	}
}
