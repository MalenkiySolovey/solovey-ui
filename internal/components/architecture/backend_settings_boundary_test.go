package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendSettingsUseCanonicalCryptoAndCandidateStorage(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, target := range []string{backendModulePath + "/settings/catalog", backendModulePath + "/settings/schema", backendModulePath + "/settings/crypto", backendModulePath + "/settings/store", backendModulePath + "/settings/manager", backendModulePath + "/security/secretbox"} {
		allowed[target] = true
	}
	for _, target := range []string{backendModulePath + "/storage/model", backendModulePath + "/storage/sqlite", backendModulePath + "/security/crypto", "gorm.io/gorm"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{backendModulePath + "/settings/catalog", backendModulePath + "/settings/schema", backendModulePath + "/settings/crypto", backendModulePath + "/settings/store", backendModulePath + "/settings/manager", backendModulePath + "/security/secretbox"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Fatalf("settings owner %s imports Product runtime or a second key/database owner: %s", target, strings.Join(violations, ", "))
		}
	}
}
