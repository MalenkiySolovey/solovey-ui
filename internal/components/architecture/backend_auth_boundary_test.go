package architecture

import (
	"sort"
	"strings"
	"testing"
)

func TestBackendSecurityValuesHaveNoStorageOrRuntimeDependency(t *testing.T) {
	root := moduleRoot(t)
	allowed := map[string]bool{backendModulePath + "/security": true, backendModulePath + "/audit": true}
	for name := range goListDependencies(t, root, "github.com/MalenkiySolovey/solovey-host/auditrecord") {
		allowed[name] = true
	}
	for _, target := range []string{backendModulePath + "/security", backendModulePath + "/audit"} {
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
	for _, name := range []string{"backend/http/auth", "backend/security", "backend/audit", "backend/security/clientidentity"} {
		allowed[backendModulePath+"/"+strings.TrimPrefix(name, "backend/")] = true
	}
	for _, target := range []string{"github.com/gin-gonic/gin", "github.com/MalenkiySolovey/solovey-host/auditrecord", "github.com/MalenkiySolovey/solovey-host/redact"} {
		for name := range goListDependencies(t, root, target) {
			allowed[name] = true
		}
	}
	var violations []string
	for name, info := range goListDependencies(t, root, backendModulePath+"/http/auth") {
		if !info.Standard && !allowed[name] {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("auth routes pull storage, execution or unrelated owners: %s", strings.Join(violations, ", "))
	}
}

func TestRequestClientIdentityHasOnlyStandardLibraryClosure(t *testing.T) {
	root := moduleRoot(t)
	for name, info := range goListDependencies(t, root, backendModulePath+"/security/clientidentity") {
		if !info.Standard && name != backendModulePath+"/security/clientidentity" {
			t.Errorf("request client identity imports an external implementation %s", name)
		}
	}
}
