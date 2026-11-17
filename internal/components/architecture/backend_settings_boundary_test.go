package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendSettingsUseCanonicalCryptoAndCandidateStorage(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, target := range []string{modulePath + "/backend/settings/catalog", modulePath + "/backend/settings/schema", modulePath + "/backend/settings/crypto", modulePath + "/backend/settings/store", modulePath + "/backend/settings/manager", modulePath + "/backend/security/secretbox"} {
		allowed[target] = true
	}
	for _, target := range []string{"./backend/storage/model", "./backend/storage/sqlite", "./backend/security/crypto", "gorm.io/gorm"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{"./backend/settings/catalog", "./backend/settings/schema", "./backend/settings/crypto", "./backend/settings/store", "./backend/settings/manager", "./backend/security/secretbox"} {
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
