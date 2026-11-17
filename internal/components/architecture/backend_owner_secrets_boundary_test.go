package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendOwnedSecretsUsePublishedReferencesAndCandidateStorage(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{modulePath + "/backend/secrets/ownedvalue": true, modulePath + "/backend/secrets/ownerdb": true, modulePath + "/backend/storage/jsonvalue": true}
	for _, target := range []string{"./backend/settings/store", "./backend/audit/record", "github.com/MalenkiySolovey/solovey-host/secretref"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{"./backend/secrets/ownedvalue", "./backend/secrets/ownerdb", "./backend/storage/jsonvalue"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Fatalf("owner secret %s imports Product-private authority: %s", target, strings.Join(violations, ", "))
		}
	}
}
