package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestAccountingAndIPPolicyExcludeProductRuntimeAndProviderOwners(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{
		"backend/accounting", "backend/accounting/ipmonitor", "backend/storage/model",
		"backend/storage/sqlite", "backend/storage/hooks", "backend/entityidentity", "backend/security", "backend/realtime",
	} {
		allowed[modulePath+"/"+name] = true
	}
	for _, target := range []string{
		"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3",
		"golang.org/x/sync/singleflight", "github.com/MalenkiySolovey/solovey-host/redact",
	} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, "./backend/accounting") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Panel accounting imports Product runtime, provider or unrelated owner: %s", strings.Join(violations, ", "))
	}
}
