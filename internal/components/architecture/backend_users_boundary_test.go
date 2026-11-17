package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestUserOwnerExcludesProductRuntimeAndProviderImplementation(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{
		"backend/users", "backend/storage/model", "backend/storage/sqlite", "backend/entityidentity", "backend/security",
		"backend/security/uuid", "backend/security/crypto", "backend/security/secretbox",
		"backend/users/credentials", "backend/users/devices", "backend/secrets/ownerdb", "backend/secrets/ownedvalue",
		"backend/settings/catalog", "backend/settings/crypto", "backend/settings/store", "backend/audit", "backend/audit/record", "backend/storage/entityidentity", "backend/storage/entityorder", "backend/storage/saveidentity", "backend/storage/jsonvalue",
	} {
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3", "github.com/gofrs/uuid/v5", "filippo.io/edwards25519", "golang.org/x/crypto/hkdf", "github.com/MalenkiySolovey/solovey-host/auditrecord", "github.com/MalenkiySolovey/solovey-host/secretref", "github.com/MalenkiySolovey/solovey-host/redact", "github.com/MalenkiySolovey/solovey-host/subscriptions/clientfacts", "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/users") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("VPN user owner imports Product runtime, provider or unrelated owner: %s", strings.Join(violations, ", "))
	}
}
