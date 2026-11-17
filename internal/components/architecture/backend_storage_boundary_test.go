package architecture

import (
	"sort"
	"strings"
	"testing"
)

// Panel record values must remain usable without constructing a database,
// provider runtime or ComponentHost. Transactions belong to their command owners.
func TestPanelStorageRecordsHaveIndependentTransitiveClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{"backend/storage/model", "backend/entityidentity", "backend/security"} {
		allowed[modulePath+"/"+name] = true
	}
	for _, target := range []string{"./backend/storage/model", "./backend/entityidentity"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Errorf("%s imports a storage connection, runtime or unrelated owner: %s", target, strings.Join(violations, ", "))
		}
	}
}
