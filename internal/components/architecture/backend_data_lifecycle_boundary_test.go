package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestDataLifecycleExcludesProductCompositionAndComponentRuntime(t *testing.T) {
	root := moduleRoot(t)
	for _, target := range []string{backendModulePath + "/datalifecycle", backendModulePath + "/storage/backup"} {
		var violations []string
		for name := range goListDependencies(t, root, target) {
			if strings.HasPrefix(name, modulePath+"/") {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Errorf("%s imports Product composition or another runtime owner: %s", target, strings.Join(violations, ", "))
		}
	}
}
