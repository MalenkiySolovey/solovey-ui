package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestUserAccessLifecycleExcludesProductRuntimeAndDelivery(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{
		"backend/users", "backend/storage/model", "backend/storage/sqlite", "backend/entityidentity", "backend/security",
	} {
		allowed[modulePath+"/"+name] = true
	}
	for _, target := range []string{"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, "./backend/users") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("VPN user access lifecycle imports Product runtime, delivery or unrelated owner: %s", strings.Join(violations, ", "))
	}
}
