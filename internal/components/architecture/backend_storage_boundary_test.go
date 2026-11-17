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
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{backendModulePath + "/storage/model", backendModulePath + "/entityidentity"} {
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

func TestPanelSQLiteRuntimeExcludesProductBootstrapAndProviders(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{backendModulePath + "/storage/sqlite": true}
	for _, target := range []string{"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/storage/sqlite") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Panel SQLite runtime imports bootstrap, migration or provider implementation: %s", strings.Join(violations, ", "))
	}
}

func TestPanelMigrationAuthorityExcludesProductPolicyAndHostState(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{backendModulePath + "/storage/migration": true}
	for _, target := range []string{"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/storage/migration") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Panel migration authority imports Product policy, Host state or unrelated owner: %s", strings.Join(violations, ", "))
	}
}

func TestPanelStorageBootstrapExcludesProductCompositionAndProviders(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{
		"backend/storage/bootstrap", "backend/storage/model", "backend/storage/sqlite",
		"backend/storage/sqliteident", "backend/entityidentity", "backend/security",
		"backend/security/password", "backend/security/crypto", "backend/settings/catalog",
	} {
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{
		"gorm.io/gorm", "gorm.io/driver/sqlite", "github.com/mattn/go-sqlite3",
		"golang.org/x/crypto/argon2", "golang.org/x/crypto/bcrypt", "golang.org/x/crypto/hkdf",
	} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/storage/bootstrap") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("Panel storage bootstrap imports Product composition, provider or unrelated owner: %s", strings.Join(violations, ", "))
	}
}
