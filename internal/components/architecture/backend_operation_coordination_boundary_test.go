package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendOperationCoordinationPreservesNarrowDurableOwners(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{
		modulePath + "/backend/operations/coordination": true,
	}
	for _, target := range []string{
		"github.com/MalenkiySolovey/solovey-host/operationref",
		"./backend/storage/model",
		"./backend/storage/sqlite",
		"gorm.io/gorm",
	} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, "./backend/operations/coordination") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Backend operation coordination imports Product execution or another authority: %s", strings.Join(violations, ", "))
	}
}
