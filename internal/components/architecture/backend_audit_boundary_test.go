package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendAuditUsesCanonicalHostRedactionAndPanelStorage(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{backendModulePath + "/audit": true, backendModulePath + "/audit/record": true, backendModulePath + "/audit/store": true}
	for _, target := range []string{backendModulePath + "/storage/model", backendModulePath + "/storage/sqlite", "github.com/MalenkiySolovey/solovey-host/auditrecord", "github.com/MalenkiySolovey/solovey-host/redact", "gorm.io/gorm"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	for _, target := range []string{backendModulePath + "/audit", backendModulePath + "/audit/record", backendModulePath + "/audit/store"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Fatalf("audit owner %s imports Product runtime or another sink implementation: %s", target, strings.Join(violations, ", "))
		}
	}
}
