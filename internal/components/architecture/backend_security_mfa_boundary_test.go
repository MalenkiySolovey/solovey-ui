package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendMFAHasOnlySecurityAndPanelStorageClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{
		backendModulePath + "/security/mfa": true,
	}
	for _, target := range []string{
		backendModulePath + "/security",
		backendModulePath + "/security/crypto",
		backendModulePath + "/security/password",
		backendModulePath + "/security/totp",
		backendModulePath + "/storage/model",
		backendModulePath + "/storage/sqlite",
		"gorm.io/gorm",
	} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/security/mfa") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Backend MFA imports Product runtime or unrelated owners: %s", strings.Join(violations, ", "))
	}
}
