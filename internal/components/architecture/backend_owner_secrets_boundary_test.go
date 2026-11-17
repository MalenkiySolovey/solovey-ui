package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendOwnedSecretsUsePublishedReferencesAndCandidateStorage(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{backendModulePath + "/secrets/ownedvalue": true, backendModulePath + "/secrets/ownerdb": true, backendModulePath + "/storage/jsonvalue": true}
	for _, target := range []string{backendModulePath + "/settings/store", backendModulePath + "/audit/record", "github.com/MalenkiySolovey/solovey-host/secretref"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{backendModulePath + "/secrets/ownedvalue", backendModulePath + "/secrets/ownerdb", backendModulePath + "/storage/jsonvalue"} {
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
