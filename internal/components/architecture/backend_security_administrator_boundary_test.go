package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendAdministratorAndTokenOwnersExcludeProductRuntime(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, target := range []string{"./backend/security", "./backend/security/crypto", "./backend/security/password", "./backend/security/mfa", "./backend/storage/model", "./backend/storage/sqlite", "./backend/audit", "gorm.io/gorm"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{"./backend/security/accounts", "./backend/security/tokens"} {
		allowed[modulePath+strings.TrimPrefix(target, ".")] = true
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Fatalf("administrator owner %s imports Product runtime or unrelated owners: %s", target, strings.Join(violations, ", "))
		}
	}
}
