package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendSecurityValuesHaveNoStorageOrRuntimeDependency(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{modulePath + "/backend/security": true, modulePath + "/backend/audit": true}
	for name := range goListDependencies(t, root, "github.com/MalenkiySolovey/solovey-host/auditrecord") {
		allowed[name] = true
	}
	for _, target := range []string{"./backend/security", "./backend/audit"} {
		var violations []string
		for name, info := range goListDependencies(t, root, target) {
			if !info.Standard && !allowed[name] {
				violations = append(violations, name)
			}
		}
		if len(violations) > 0 {
			sort.Strings(violations)
			t.Errorf("%s imports outside its value owner: %s", target, strings.Join(violations, ", "))
		}
	}
}

func TestAuthenticationRoutesHaveNarrowTransitiveOwnerClosure(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{}
	for _, name := range []string{"api/auth", "backend/security", "backend/audit", "internal/httpsecurity/clientidentity", "logger", "util/redact"} {
		allowed[modulePath+"/"+name] = true
	}
	for _, target := range []string{"github.com/gin-gonic/gin", "github.com/MalenkiySolovey/solovey-host/auditrecord", "github.com/MalenkiySolovey/solovey-host/redact"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, "./api/auth") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("auth routes pull storage, execution or unrelated owners: %s", strings.Join(violations, ", "))
	}
}
